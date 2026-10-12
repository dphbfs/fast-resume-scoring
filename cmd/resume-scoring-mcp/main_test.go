package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true,
		"localhost:8080": true,
		"[::1]:8080":     true,
		"0.0.0.0:8080":   false,
		":8080":          false,
		"10.0.0.5:8080":  false,
		"nonsense":       false,
	} {
		if got := loopback(addr); got != want {
			t.Errorf("loopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestAuthorize(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, tt := range []struct {
		name, token, header string
		want                int
	}{
		{"no token configured", "", "", http.StatusNoContent},
		{"missing", "s3cret", "", http.StatusUnauthorized},
		{"wrong", "s3cret", "Bearer nope", http.StatusUnauthorized},
		{"right", "s3cret", "Bearer s3cret", http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			authorize(tt.token, ok).ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestRunUsage(t *testing.T) {
	t.Setenv("MCP_HTTP_TOKEN", "")
	for _, args := range [][]string{
		{"-transport", "sse"},
		{"-transport", "http", "-addr", "0.0.0.0:8080"},
		{"extra"},
	} {
		if code := run(args, &discard{}); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
	}
}

type discard struct{}

func (*discard) Write(p []byte) (int, error) { return len(p), nil }
