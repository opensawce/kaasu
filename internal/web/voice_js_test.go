package web

import (
	"os"
	"strings"
	"testing"
)

func TestVoiceJSHasNoExternalCalls(t *testing.T) {
	b, err := os.ReadFile("static/voice.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "http://") || strings.Contains(s, "https://") || strings.Contains(s, "fetch(") {
		t.Error("voice.js must not make network calls")
	}
	if !strings.Contains(s, "list.length > 50") {
		t.Error("the spoken list must stay bounded")
	}
}
