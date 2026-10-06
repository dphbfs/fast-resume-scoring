// Package providererr turns an AI provider's error response into a short,
// safe summary for errors and logs: only allowlisted message fields, the
// request ID, no control characters, no key-like strings, at most
// MaxMessageBytes.
package providererr

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxMessageBytes caps the summarized message.
const MaxMessageBytes = 512

// requestIDHeaders are checked in order for the provider's request ID.
var requestIDHeaders = []string{"X-Request-Id", "Request-Id", "X-Generation-Id", "Cf-Ray"}

// secretLike matches API keys and bearer tokens a provider might echo.
var secretLike = regexp.MustCompile(`(?i)(bearer\s+\S+|\bsk-[a-z0-9_\-]{8,}|\b[a-z0-9_\-]{32,}\b)`)

// Summary is the safe part of an error response.
type Summary struct {
	Message   string
	RequestID string
}

// Summarize extracts the message and request ID from an error response.
func Summarize(header http.Header, body []byte) Summary {
	s := Summary{Message: truncate(redact(printable(message(body))), MaxMessageBytes)}
	for _, h := range requestIDHeaders {
		if v := printable(header.Get(h)); v != "" {
			s.RequestID = truncate(v, 64)
			break
		}
	}
	return s
}

// message returns an allowlisted message field of a JSON body, or the body
// itself when it is not JSON.
func message(body []byte) string {
	var v map[string]any
	if json.Unmarshal(body, &v) != nil {
		return string(body)
	}
	if e, ok := v["error"].(map[string]any); ok {
		if m, ok := e["message"].(string); ok {
			return m
		}
	}
	for _, key := range []string{"error", "message", "detail"} {
		switch m := v[key].(type) {
		case string:
			return m
		case nil:
		default:
			// e.g. a validation detail list: keep its JSON, still capped.
			if raw, err := json.Marshal(m); err == nil {
				return string(raw)
			}
		}
	}
	return "(no message)"
}

// printable collapses whitespace and drops non-printable runes.
func printable(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// redact replaces key-like strings.
func redact(s string) string {
	return secretLike.ReplaceAllString(s, "[redacted]")
}

// truncate cuts s to at most n bytes on a rune boundary, marking the cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
