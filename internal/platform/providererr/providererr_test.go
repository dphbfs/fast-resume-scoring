package providererr

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSummarize(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"openai shape", `{"error":{"message":"Rate limit reached","type":"tokens","param":null}}`, "Rate limit reached"},
		{"string error", `{"error":"model overloaded"}`, "model overloaded"},
		{"detail list", `{"detail":[{"loc":["body","questions"],"msg":"field required"}]}`, `[{"loc":["body","questions"],"msg":"field required"}]`},
		{"message field", `{"message":"bad\nrequest\t now"}`, "bad request now"},
		{"no allowlisted field", `{"state":"the whole resume text","trace":"..."}`, "(no message)"},
		{"plain text", "upstream connect error\r\n", "upstream connect error"},
		{"bearer echoed", `{"error":"invalid header Authorization: Bearer abc.def.ghi"}`, "invalid header Authorization: [redacted]"},
		{"api key echoed", `{"error":"key sk-or-v1-0123456789abcdef is over its limit"}`, "key [redacted] is over its limit"},
		{"control chars", "bad\x00\x07 thing", "bad thing"},
	} {
		got := Summarize(http.Header{}, []byte(tt.body)).Message
		if got != tt.want {
			t.Errorf("%s: message = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSummarizeCapsAndRequestID(t *testing.T) {
	h := http.Header{}
	h.Set("X-Request-Id", "3f1c2a9e-7b4d-4c1e-9a6f-2b8d5e7c1a04")
	s := Summarize(h, []byte(strings.Repeat("é", 1000)))
	if len(s.Message) > MaxMessageBytes || !utf8.ValidString(s.Message) || !strings.HasSuffix(s.Message, "…") {
		t.Errorf("message not capped cleanly: %d bytes, valid %v", len(s.Message), utf8.ValidString(s.Message))
	}
	if s.RequestID != "3f1c2a9e-7b4d-4c1e-9a6f-2b8d5e7c1a04" {
		t.Errorf("request id = %q", s.RequestID)
	}
}
