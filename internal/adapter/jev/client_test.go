package jev_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev/jevtest"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

func newClient(t *testing.T, url string, m port.Metrics) *jev.Client {
	t.Helper()
	c, err := jev.New(config.Jev{
		APIKey:         "test-key",
		BaseURL:        url,
		Model:          "jev-latest",
		MaxConcurrency: 2,
		MaxRetries:     2,
		Timeout:        5 * time.Second,
	}, m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	jev.SetBackoff(c, func(int) time.Duration { return time.Millisecond })
	return c
}

var urgentQuestion = map[string]port.Question{
	"is_urgent": {Type: port.Noul, Instructions: "Does this convey urgency?"},
}

func TestClassifyMapsAnswers(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(string, jev.WireQuestion) jev.WireAnswer {
		return jevtest.Noul(0.95)
	}))
	m := metrics.NewRecorder()
	c := newClient(t, srv.URL, m)

	resp, err := c.Classify(context.Background(), port.ClassifyRequest{
		State:     "Help! Payouts failing for 3 days.",
		Questions: urgentQuestion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Model != jevtest.Model {
		t.Errorf("Model = %q, want %q", resp.Model, jevtest.Model)
	}
	a := resp.Answers["is_urgent"]
	if a.Type != port.Noul || a.Noul == nil || *a.Noul != 0.95 {
		t.Errorf("answer = %+v, want noul 0.95", a)
	}

	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Model != "jev-latest" || reqs[0].Questions["is_urgent"].Type != "noul" {
		t.Errorf("server got %+v", reqs)
	}
	if got := m.Summary().Counters["jev.input_tokens"]; got != 100 {
		t.Errorf("jev.input_tokens = %d, want 100", got)
	}
}

func TestClassifyAcceptsOpenRouterResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"gen-dec-1","model":"typesafe/jev-1.13-20260917","provider":"TypeSafe",` +
			`"answers":{"is_urgent":{"type":"noul","noul":0.98}},` +
			`"usage":{"input_tokens":275,"output_tokens":20,"cost":0.00003}}`))
	}))
	defer srv.Close()
	m := metrics.NewRecorder()
	c := newClient(t, srv.URL, m)

	resp, err := c.Classify(context.Background(), port.ClassifyRequest{State: "x", Questions: urgentQuestion})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Model != "typesafe/jev-1.13-20260917" || *resp.Answers["is_urgent"].Noul != 0.98 {
		t.Errorf("resp = %+v", resp)
	}
	if got := m.Summary().Counters["jev.cost_micro_usd"]; got != 30 {
		t.Errorf("jev.cost_micro_usd = %d, want 30", got)
	}
}

func TestClassifyRetriesRetryableStatus(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, jev.StatusOverloaded} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := jevtest.NewServer(t, func(call int, req jev.WireRequest) jevtest.Reply {
				if call == 0 {
					return jevtest.Reply{Status: status, Body: "slow down"}
				}
				return jevtest.Reply{Answers: map[string]jev.WireAnswer{"is_urgent": jevtest.Noul(0.1)}}
			})
			m := metrics.NewRecorder()
			c := newClient(t, srv.URL, m)

			if _, err := c.Classify(context.Background(), port.ClassifyRequest{State: "x", Questions: urgentQuestion}); err != nil {
				t.Fatal(err)
			}
			if n := len(srv.Requests()); n != 2 {
				t.Errorf("requests = %d, want 2", n)
			}
			if got := m.Summary().Counters["jev.retries"]; got != 1 {
				t.Errorf("jev.retries = %d, want 1", got)
			}
		})
	}
}

func TestClassifyDoesNotRetryValidationError(t *testing.T) {
	srv := jevtest.NewServer(t, func(int, jev.WireRequest) jevtest.Reply {
		return jevtest.Reply{Status: http.StatusUnprocessableEntity, Body: `{"detail":"bad criteria"}`}
	})
	c := newClient(t, srv.URL, metrics.NewRecorder())

	_, err := c.Classify(context.Background(), port.ClassifyRequest{State: "x", Questions: urgentQuestion})
	var apiErr *jev.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("err = %v, want APIError 422", err)
	}
	if apiErr.Message != "bad criteria" || err.Error() != "jev: HTTP 422: bad criteria" {
		t.Errorf("error = %q, want the sanitized detail only", err)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("requests = %d, want 1 (no retry)", n)
	}
}

func TestClassifyGivesUpAfterMaxRetries(t *testing.T) {
	srv := jevtest.NewServer(t, func(int, jev.WireRequest) jevtest.Reply {
		return jevtest.Reply{Status: jev.StatusOverloaded}
	})
	c := newClient(t, srv.URL, metrics.NewRecorder())

	if _, err := c.Classify(context.Background(), port.ClassifyRequest{State: "x", Questions: urgentQuestion}); err == nil {
		t.Fatal("want error, got nil")
	}
	if n := len(srv.Requests()); n != 3 {
		t.Errorf("requests = %d, want 3 (1 + 2 retries)", n)
	}
}

func TestClassifyRejectsMissingAnswer(t *testing.T) {
	srv := jevtest.NewServer(t, func(int, jev.WireRequest) jevtest.Reply {
		return jevtest.Reply{Answers: map[string]jev.WireAnswer{}}
	})
	c := newClient(t, srv.URL, metrics.NewRecorder())

	if _, err := c.Classify(context.Background(), port.ClassifyRequest{State: "x", Questions: urgentQuestion}); err == nil {
		t.Fatal("want error for missing answer, got nil")
	}
}

func TestClassifyStopsWhenCanceledDuringHTTP(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A hung provider: no answer until the test ends.
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs first: lets Close finish
	c := newClient(t, srv.URL, metrics.NewRecorder())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Classify(ctx, port.ClassifyRequest{State: "x", Questions: urgentQuestion})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Classify took %s after cancel, want prompt return", elapsed)
	}
}

func TestClassifyStopsWhenCanceledDuringBackoff(t *testing.T) {
	srv := jevtest.NewServer(t, func(int, jev.WireRequest) jevtest.Reply {
		return jevtest.Reply{Status: http.StatusServiceUnavailable, Body: "down"}
	})
	c := newClient(t, srv.URL, metrics.NewRecorder())
	jev.SetBackoff(c, func(int) time.Duration { return time.Hour })

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	start := time.Now()
	_, err := c.Classify(ctx, port.ClassifyRequest{State: "x", Questions: urgentQuestion})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Classify took %s, want it to stop waiting for the backoff", elapsed)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("requests = %d, want 1 (no retry after cancel)", n)
	}
}
