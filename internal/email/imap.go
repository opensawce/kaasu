package email

import (
	"context"
	"fmt"
	"strconv"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// IMAPFetcher reads unseen mail over TLS with an app password. The password is read for each poll.
type IMAPFetcher struct {
	Addr     string // host:port, TLS
	User     string
	Mailbox  string
	Password func() (string, error)
}

const (
	maxPerPoll    = 50      // messages read per poll; the rest wait for the next one
	maxRawMessage = 2 << 20 // bytes read per message; a longer one is cut and then fails DKIM
)

// connect dials, logs in and selects the mailbox. Cancelling ctx closes the connection, which unblocks any call on it.
func (f IMAPFetcher) connect(ctx context.Context) (*imapclient.Client, func(), error) {
	pass, err := f.Password()
	if err != nil {
		return nil, nil, fmt.Errorf("read mail password: %w", err)
	}
	c, err := imapclient.DialTLS(f.Addr, nil) // default 30 s dial timeout
	if err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	closeAll := func() { close(done); _ = c.Close() }
	if err := c.Login(f.User, pass).Wait(); err != nil {
		closeAll()
		return nil, nil, err
	}
	if _, err := c.Select(f.Mailbox, nil).Wait(); err != nil {
		closeAll()
		return nil, nil, err
	}
	return c, closeAll, nil
}

func (f IMAPFetcher) Fetch(ctx context.Context) ([]RawMessage, error) {
	c, closeAll, err := f.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer closeAll()
	data, err := c.UIDSearch(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
	if err != nil {
		return nil, err
	}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	if len(uids) > maxPerPoll {
		uids = uids[:maxPerPoll]
	}
	bodySection := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: maxRawMessage}}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{bodySection}}).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]RawMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, RawMessage{UID: fmt.Sprint(m.UID), Raw: m.FindBodySection(bodySection)})
	}
	return out, nil
}

// MarkSeen flags handled messages \Seen so the next poll does not fetch them again.
func (f IMAPFetcher) MarkSeen(ctx context.Context, uids []string) error {
	if len(uids) == 0 {
		return nil
	}
	nums := make([]imap.UID, 0, len(uids))
	for _, u := range uids {
		n, err := strconv.ParseUint(u, 10, 32)
		if err != nil {
			return fmt.Errorf("bad uid %q: %w", u, err)
		}
		nums = append(nums, imap.UID(n))
	}
	c, closeAll, err := f.connect(ctx)
	if err != nil {
		return err
	}
	defer closeAll()
	return c.Store(imap.UIDSetNum(nums...), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close()
}
