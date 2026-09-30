package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

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
