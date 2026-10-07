package openai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
)

var discard = slog.New(slog.DiscardHandler)

func TestGenerateUnconfigured(t *testing.T) {
	c, err := New(config.Generative{MaxConcurrency: 1}, metrics.NewRecorder(), discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Generate(context.Background(), "sys", "hi"); !errors.Is(err, port.ErrGenerativeUnavailable) {
		t.Fatalf("err = %v, want ErrGenerativeUnavailable", err)
	}
}

func TestGenerate(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  A backend role.  "}}]}`))
	}))
	defer srv.Close()

	c, err := New(config.Generative{
		BaseURL: srv.URL + "/v1/", APIKey: "k", Model: "m", MaxConcurrency: 1, Timeout: 5 * time.Second,
	}, metrics.NewRecorder(), discard)
	if err != nil {
		t.Fatal(err)
	}

	out, err := c.Generate(context.Background(), "summarize", "job text")
	if err != nil {
		t.Fatal(err)
	}
	if out != "A backend role." {
		t.Errorf("out = %q, want trimmed reply", out)
	}
	if got.Model != "m" || len(got.Messages) != 2 || got.Messages[1].Content != "job text" {
		t.Errorf("request = %+v", got)
	}
}

func TestCompleteUsageAndNoEmptySystem(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"}}],
			"usage":{"prompt_tokens":120,"completion_tokens":7,"cost":0.0003}}`))
	}))
	defer srv.Close()

	rec := metrics.NewRecorder()
	c, err := New(config.Generative{BaseURL: srv.URL, Model: "m", MaxConcurrency: 1, Timeout: 5 * time.Second}, rec, discard)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Complete(context.Background(), "", "score this")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
		t.Errorf("messages = %+v, want only the user prompt", got.Messages)
	}
	if r.Usage.InputTokens != 120 || r.Usage.OutputTokens != 7 || r.Usage.Cost == nil || *r.Usage.Cost != 0.0003 {
		t.Errorf("usage = %+v", r.Usage)
	}
	s := rec.Summary()
	if s.Counters["gen.input_tokens"] != 120 || s.Counters["gen.cost_micro_usd"] != 300 {
		t.Errorf("counters = %v", s.Counters)
	}
}

func TestGenerateErrorIsSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_42")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: sk-proj-abcdef0123456789","type":"invalid_request_error"},"prompt":"the whole resume"}`))
	}))
	defer srv.Close()
	c, err := New(config.Generative{BaseURL: srv.URL, APIKey: "k", Model: "m", MaxConcurrency: 1, Timeout: 5 * time.Second},
		metrics.NewRecorder(), discard)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Generate(context.Background(), "sys", "hi")
	if err == nil {
		t.Fatal("want error")
	}
	msg := err.Error()
	if msg != "openai: HTTP 401 (request req_42): Incorrect API key provided: [redacted]" {
		t.Errorf("error = %q", msg)
	}
}
