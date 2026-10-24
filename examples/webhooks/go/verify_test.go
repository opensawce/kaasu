package verify

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The shared vectors also run in every other language example.
func TestSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Secret, Body, Header string
		Timestamp            int64
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(v.Timestamp, 0)
	if err := Check(v.Secret, v.Header, []byte(v.Body), now); err != nil {
		t.Fatalf("genuine delivery rejected: %v", err)
	}
	if err := Check(v.Secret, v.Header, []byte(v.Body+" "), now); err == nil {
		t.Error("tampered body accepted")
	}
	if err := Check(v.Secret, v.Header, []byte(v.Body), now.Add(10*time.Minute)); err == nil {
		t.Error("stale delivery accepted")
	}
}
