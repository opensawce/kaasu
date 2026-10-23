// Package verify shows how a shop backend checks a Kaasu webhook in Go.
package verify

import (
	"time"

	"github.com/opensawce/kaasu/internal/webhooks"
)

// Check returns nil only for a genuine, fresh delivery. Dedupe by event_id in your own store.
func Check(secret, header string, body []byte, now time.Time) error {
	return webhooks.Verify(secret, header, body, now, 5*time.Minute)
}
