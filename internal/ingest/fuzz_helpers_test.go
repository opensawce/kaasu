package ingest

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/devices"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/testutil"
)

func setupFuzz(t *testing.T) http.Handler {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	return Handler(Deps{Store: st, Clock: clk, Devices: devices.New(st, clk, "h"), Evidence: evidence.New(clk, &testutil.Seq{})})
}

func newBody(b []byte) io.Reader { return bytes.NewReader(b) }
