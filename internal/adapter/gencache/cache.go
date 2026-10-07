// Package gencache wraps an AIGenerativeClient with a file cache, so
// generated text (the Job Summary) is identical across eval runs and
// Validation inputs stay fixed while tuning. Concurrent misses for the same
// key make one generation; entries are written atomically and privately.
package gencache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sync/singleflight"

	"github.com/dphbfs/fast-resume-scoring/internal/platform/fsutil"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
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
	group   singleflight.Group
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

	text, err, _ := c.group.Do(path, func() (any, error) {
		// Another caller may have stored it while this one waited.
		if raw, err := os.ReadFile(path); err == nil {
			return string(raw), nil
		}
		c.metrics.Add("gencache.misses", 1)
		text, err := c.inner.Generate(ctx, system, prompt)
		if err != nil || text == "" {
			return text, err
		}
		if err := fsutil.WriteFileAtomic(path, []byte(text)); err != nil {
			return "", fmt.Errorf("gencache: %w", err)
		}
		return text, nil
	})
	s, _ := text.(string) // the function always returns a string
	return s, err
}
