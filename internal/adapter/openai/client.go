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
}

// Generate returns the model's reply to prompt under the system message.
func (c *Client) Generate(ctx context.Context, system, prompt string) (string, error) {
	if c.cfg.Model == "" {
		return "", port.ErrGenerativeUnavailable
	}
	body, err := json.Marshal(chatRequest{
		Model: c.cfg.Model,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("openai: encode request: %w", err)
	}

	var out string
	err = c.limiter.Do(ctx, func(ctx context.Context) error {
		start := time.Now()
		out, err = c.post(ctx, body)
		c.metrics.Add("gen.calls", 1)
		c.metrics.ObserveDuration("gen.latency", time.Since(start))
		return err
	})
	if err != nil {
		c.metrics.Add("gen.errors", 1)
		return "", err
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, body []byte) (string, error) {
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("openai: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, raw)
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("openai: decode response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("openai: response has no choices")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}
