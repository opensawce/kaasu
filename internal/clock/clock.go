// Package clock lets code ask for the time without calling time.Now, so tests and dev mode can control it.
package clock

import (
	"sync"
	"time"
)

// Clock is the only way production code reads the time.
type Clock interface{ Now() time.Time }

// Real is wall-clock time in UTC.
type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Fake is a clock that only moves when told to.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

func NewFake(start time.Time) *Fake { return &Fake{t: start.UTC()} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	f.t = t.UTC()
	f.mu.Unlock()
}
