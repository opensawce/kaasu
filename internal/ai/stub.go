package ai

import (
	"context"
	"encoding/json"
)

// Stub is a provider that answers with a fixed body and records what it was sent. Tests use it; nothing real is called.
type Stub struct {
	Answer json.RawMessage
	Seen   []Request
}

func (s *Stub) Name() string { return "stub" }

func (s *Stub) Complete(_ context.Context, r Request) (Response, error) {
	s.Seen = append(s.Seen, r)
	return Response{JSON: s.Answer, Model: "stub-1", InputTokens: 100, OutputTokens: 20}, nil
}
