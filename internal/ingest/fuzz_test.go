package ingest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// FuzzIngestRequest: arbitrary bodies and headers never panic, and an unsigned request is never accepted.
func FuzzIngestRequest(f *testing.F) {
	f.Add([]byte(sampleAlert), "dev_x", "nonce_aaaaaaaaaaaaaaaa", "1767261600", "c2ln")
	f.Fuzz(func(t *testing.T, body []byte, dev, nonce, ts, sig string) {
		st := setupFuzz(t)
		req := httptest.NewRequest(http.MethodPost, "/ingest/v1/alerts", newBody(body))
		req.Header.Set("X-Kaasu-Device", dev)
		req.Header.Set("X-Kaasu-Nonce", nonce)
		req.Header.Set("X-Kaasu-Timestamp", ts)
		req.Header.Set("X-Kaasu-Signature", sig)
		rec := httptest.NewRecorder()
		st.ServeHTTP(rec, req)
		if rec.Code/100 == 2 {
			t.Fatalf("accepted an unsigned request: %d", rec.Code)
		}
	})
}
