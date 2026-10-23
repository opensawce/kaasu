package email

import (
	"context"
	"fmt"

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

func (f IMAPFetcher) Fetch(ctx context.Context) ([]RawMessage, error) {
	pass, err := f.Password()
	if err != nil {
		return nil, fmt.Errorf("read mail password: %w", err)
	}
	c, err := imapclient.DialTLS(f.Addr, nil)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if err := c.Login(f.User, pass).Wait(); err != nil {
		return nil, err
	}
	if _, err := c.Select(f.Mailbox, nil).Wait(); err != nil {
		return nil, err
	}
	data, err := c.Search(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
	if err != nil {
		return nil, err
	}
	seqs := data.AllSeqNums()
	if len(seqs) == 0 {
		return nil, nil
	}
	bodySection := &imap.FetchItemBodySection{Peek: true}
	msgs, err := c.Fetch(imap.SeqSetNum(seqs...), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{bodySection}}).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]RawMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, RawMessage{UID: fmt.Sprint(m.UID), Raw: m.FindBodySection(bodySection)})
	}
	return out, nil
}
