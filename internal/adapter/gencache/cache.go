// Package gencache wraps an AIGenerativeClient with a file cache, so
// generated text (the Job Summary) is identical across eval runs and
// Validation inputs stay fixed while tuning.
package gencache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// Dir is the directory holding cached generations.
type Dir string

// Inner is the wrapped client. It is a distinct type so dependency injection
// can tell it apart from the Client that wraps it.
type Inner interface{ port.AIGenerativeClient }

// Client serves generations from Dir, calling the inner client on a miss.
type Client struct {
	inner   Inner
	dir     string
	metrics port.Metrics
}

var _ port.AIGenerativeClient = (*Client)(nil)

// New wraps inner with a cache in dir.
func New(inner Inner, dir Dir, m port.Metrics) *Client {
	return &Client{inner: inner, dir: string(dir), metrics: m}
}

// Generate returns the cached text for (system, prompt), generating and
// storing it on a miss. Errors, including an unconfigured inner client, are
// not cached.
func (c *Client) Generate(ctx context.Context, system, prompt string) (string, error) {
	sum := sha256.Sum256([]byte(system + "\x00" + prompt))
	path := filepath.Join(c.dir, hex.EncodeToString(sum[:8])+".txt")

	if raw, err := os.ReadFile(path); err == nil {
		c.metrics.Add("gencache.hits", 1)
		return string(raw), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("gencache: %w", err)
	}

	c.metrics.Add("gencache.misses", 1)
	text, err := c.inner.Generate(ctx, system, prompt)
	if err != nil || text == "" {
		return text, err
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return "", fmt.Errorf("gencache: %w", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("gencache: %w", err)
	}
	return text, nil
}
