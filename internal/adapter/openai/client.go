// Package openai implements port.AIGenerativeClient against any
// OpenAI-compatible chat completions API.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/limiter"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// Client calls POST {BaseURL}/chat/completions through its own Limiter.
type Client struct {
	cfg     config.Generative
	http    *http.Client
	limiter *limiter.Limiter
	metrics port.Metrics
	log     *slog.Logger
}

var _ port.AIGenerativeClient = (*Client)(nil)

// New builds a Client from cfg. A Client with an empty Model is valid; its
// Generate returns port.ErrGenerativeUnavailable.
func New(cfg config.Generative, m port.Metrics, log *slog.Logger) (*Client, error) {
	lim, err := limiter.New(cfg.MaxConcurrency)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	return &Client{
		cfg:     cfg,
		http:    &http.Client{Timeout: cfg.Timeout},
		limiter: lim,
		metrics: m,
		log:     log.With("component", "openai"),
	}, nil
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

// Usage is the token usage of one call. Cost (USD) is only set when the
// provider reports it (OpenRouter does).
type Usage struct {
	InputTokens  int      `json:"prompt_tokens"`
	OutputTokens int      `json:"completion_tokens"`
	Cost         *float64 `json:"cost,omitempty"`
}

// Reply is one chat completion with its usage and the time the HTTP call
// took (queueing in the limiter excluded).
type Reply struct {
	Text    string
	Usage   Usage
	Latency time.Duration
}

// Generate returns the model's reply to prompt under the system message.
func (c *Client) Generate(ctx context.Context, system, prompt string) (string, error) {
	r, err := c.Complete(ctx, system, prompt)
	return r.Text, err
}

// Complete is Generate plus the call's usage and latency. An empty system
// sends the prompt as the only message.
func (c *Client) Complete(ctx context.Context, system, prompt string) (Reply, error) {
	if c.cfg.Model == "" {
		return Reply{}, port.ErrGenerativeUnavailable
	}
	var msgs []message
	if system != "" {
		msgs = append(msgs, message{Role: "system", Content: system})
	}
	msgs = append(msgs, message{Role: "user", Content: prompt})
	body, err := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: msgs})
	if err != nil {
		return Reply{}, fmt.Errorf("openai: encode request: %w", err)
	}

	var out Reply
	err = c.limiter.Do(ctx, func(ctx context.Context) error {
		start := time.Now()
		out, err = c.post(ctx, body)
		out.Latency = time.Since(start)
		c.metrics.Add("gen.calls", 1)
		c.metrics.ObserveDuration("gen.latency", out.Latency)
		return err
	})
	if err != nil {
		c.metrics.Add("gen.errors", 1)
		return Reply{}, err
	}
	c.metrics.Add("gen.input_tokens", int64(out.Usage.InputTokens))
	c.metrics.Add("gen.output_tokens", int64(out.Usage.OutputTokens))
	if out.Usage.Cost != nil {
		c.metrics.Add("gen.cost_micro_usd", int64(math.Round(*out.Usage.Cost*1e6)))
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, body []byte) (Reply, error) {
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Reply{}, fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Reply{}, fmt.Errorf("openai: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return Reply{}, fmt.Errorf("openai: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Reply{}, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, raw)
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return Reply{}, fmt.Errorf("openai: decode response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return Reply{}, fmt.Errorf("openai: response has no choices")
	}
	return Reply{Text: strings.TrimSpace(cr.Choices[0].Message.Content), Usage: cr.Usage}, nil
}
