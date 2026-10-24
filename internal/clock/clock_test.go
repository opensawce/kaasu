package clock

import (
	"testing"
	"time"
)

func TestFakeAdvanceAndSet(t *testing.T) {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	f := NewFake(start)
	if !f.Now().Equal(start) {
		t.Fatalf("now=%v", f.Now())
	}
	f.Advance(15 * time.Minute)
	if want := start.Add(15 * time.Minute); !f.Now().Equal(want) {
		t.Errorf("after advance now=%v want %v", f.Now(), want)
	}
	later := start.Add(24 * time.Hour)
	f.Set(later)
	if !f.Now().Equal(later) {
		t.Errorf("after set now=%v", f.Now())
	}
}

func TestRealIsUTCAndMoves(t *testing.T) {
	a := Real{}.Now()
	if a.Location() != time.UTC {
		t.Errorf("location=%v", a.Location())
	}
	if time.Since(a) > time.Second {
		t.Error("real clock far from time.Now")
	}
}

func TestFakeConcurrentAdvance(t *testing.T) {
	f := NewFake(time.Unix(0, 0).UTC())
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() { f.Advance(time.Second); done <- struct{}{} }()
	}
	for i := 0; i < 50; i++ {
		<-done
	}
	if got := f.Now().Unix(); got != 50 {
		t.Errorf("lost advances: %d", got)
	}
}
