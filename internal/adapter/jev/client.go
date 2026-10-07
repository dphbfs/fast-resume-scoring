// Package jev implements port.AIClassifierClient against the System One API
// (POST /v1/systemone) that serves the Jev model. Both TypeSafe
// (https://api.typesafe.ai) and OpenRouter (https://openrouter.ai/api) expose
// it with the same request and response shapes.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/limiter"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/providererr"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
)

const endpoint = "/v1/systemone"

// StatusOverloaded is TypeSafe's non-standard "overloaded" status.
const StatusOverloaded = 529

// Client calls Jev through its own concurrency Limiter.
type Client struct {
	cfg     config.Jev
	http    *http.Client
	limiter *limiter.Limiter
	metrics port.Metrics
	log     *slog.Logger
	// backoff returns the wait before retry attempt n (1-based). Replaced in
	// tests to avoid sleeping.
	backoff func(attempt int) time.Duration
}

var _ port.AIClassifierClient = (*Client)(nil)

// New builds a Client from cfg.
func New(cfg config.Jev, m port.Metrics, log *slog.Logger) (*Client, error) {
	lim, err := limiter.New(cfg.MaxConcurrency)
	if err != nil {
		return nil, fmt.Errorf("jev: %w", err)
	}
	return &Client{
		cfg:     cfg,
		http:    &http.Client{Timeout: cfg.Timeout},
		limiter: lim,
		metrics: m,
		log:     log.With("component", "jev"),
		backoff: exponentialBackoff,
	}, nil
}

// APIError is a non-2xx response from the API. It keeps only the
// provider's sanitized message and request ID, never the raw body.
type APIError struct {
	Status    int
	Message   string
	RequestID string
}

func (e *APIError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("jev: HTTP %d (request %s): %s", e.Status, e.RequestID, e.Message)
	}
	return fmt.Sprintf("jev: HTTP %d: %s", e.Status, e.Message)
}

// Retryable reports whether the request may succeed if sent again.
func (e *APIError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status == StatusOverloaded || e.Status >= 500
}

// Classify sends one request and returns an answer for every question.
func (c *Client) Classify(ctx context.Context, req port.ClassifyRequest) (port.ClassifyResponse, error) {
	body, err := json.Marshal(toWire(c.cfg.Model, req))
	if err != nil {
		return port.ClassifyResponse{}, fmt.Errorf("jev: encode request: %w", err)
	}

	var resp port.ClassifyResponse
	err = c.limiter.Do(ctx, func(ctx context.Context) error {
		resp, err = c.send(ctx, body, len(req.Questions))
		return err
	})
	if err != nil {
		c.metrics.Add("jev.errors", 1)
		return port.ClassifyResponse{}, err
	}

	if err := validateAnswers(req.Questions, resp.Answers); err != nil {
		c.metrics.Add("jev.errors", 1)
		return port.ClassifyResponse{}, err
	}
	return resp, nil
}

// send posts body, retrying retryable failures up to cfg.MaxRetries times.
func (c *Client) send(ctx context.Context, body []byte, questions int) (port.ClassifyResponse, error) {
	for attempt := 0; ; attempt++ {
		start := time.Now()
		resp, retryAfter, err := c.post(ctx, body)
		elapsed := time.Since(start)
		c.metrics.Add("jev.calls", 1)
		c.metrics.ObserveDuration("jev.latency", elapsed)

		if err == nil {
			c.metrics.Add("jev.input_tokens", int64(resp.Usage.InputTokens))
			c.metrics.Add("jev.output_tokens", int64(resp.Usage.OutputTokens))
			if resp.Usage.CostUSD != nil {
				c.metrics.Add("jev.cost_micro_usd", int64(math.Round(*resp.Usage.CostUSD*1e6)))
			}
			c.log.DebugContext(ctx, "classify",
				"model", resp.Model,
				"questions", questions,
				"input_tokens", resp.Usage.InputTokens,
				"latency_ms", elapsed.Milliseconds(),
				"attempt", attempt+1)
			return resp, nil
		}

		var apiErr *APIError
		retryable := errors.As(err, &apiErr) && apiErr.Retryable()
		if !retryable || attempt >= c.cfg.MaxRetries {
			return port.ClassifyResponse{}, err
		}

		wait := max(retryAfter, c.backoff(attempt+1))
		c.metrics.Add("jev.retries", 1)
		c.log.WarnContext(ctx, "classify retry",
			"status", apiErr.Status, "attempt", attempt+1, "wait_ms", wait.Milliseconds())

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return port.ClassifyResponse{}, ctx.Err()
		}
	}
}

// post makes one HTTP call. retryAfter is the server's Retry-After hint, if any.
func (c *Client) post(ctx context.Context, body []byte) (resp port.ClassifyResponse, retryAfter time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return resp, 0, fmt.Errorf("jev: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(req)
	if err != nil {
		return resp, 0, fmt.Errorf("jev: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 10<<20))
	if err != nil {
		return resp, 0, fmt.Errorf("jev: read response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		sum := providererr.Summarize(httpResp.Header, raw)
		return resp, parseRetryAfter(httpResp.Header.Get("Retry-After")),
			&APIError{Status: httpResp.StatusCode, Message: sum.Message, RequestID: sum.RequestID}
	}

	var w WireResponse
	if err := json.Unmarshal(raw, &w); err != nil {
		return resp, 0, fmt.Errorf("jev: decode response: %w", err)
	}
	return fromWire(w), 0, nil
}

// exponentialBackoff waits 500ms, 1s, 2s, ... (capped at 16s) plus up to 25% jitter.
func exponentialBackoff(attempt int) time.Duration {
	base := min(500*time.Millisecond<<(attempt-1), 16*time.Second)
	return base + rand.N(base/4+1) //nolint:gosec // retry jitter, not security-sensitive
}

// parseRetryAfter reads a delay in seconds; HTTP-date values are ignored.
func parseRetryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(v)
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}
