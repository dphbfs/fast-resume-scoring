package gencache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-scoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
)

type countingGen struct {
	calls int
	err   error
}

func (g *countingGen) Generate(context.Context, string, string) (string, error) {
	g.calls++
	if g.err != nil {
		return "", g.err
	}
	return "summary", nil
}

func TestCacheServesSecondCall(t *testing.T) {
	inner := &countingGen{}
	c := New(inner, Dir(t.TempDir()), metrics.NewRecorder())
	for range 3 {
		out, err := c.Generate(context.Background(), "sys", "job text")
		if err != nil || out != "summary" {
			t.Fatalf("Generate = %q, %v", out, err)
		}
	}
	if inner.calls != 1 {
		t.Errorf("inner calls = %d, want 1", inner.calls)
	}
	if _, _ = c.Generate(context.Background(), "sys", "other job"); inner.calls != 2 {
		t.Errorf("a different prompt must miss the cache")
	}
}

func TestCacheDoesNotStoreErrors(t *testing.T) {
	inner := &countingGen{err: port.ErrGenerativeUnavailable}
	c := New(inner, Dir(t.TempDir()), metrics.NewRecorder())
	for range 2 {
		if _, err := c.Generate(context.Background(), "sys", "job"); !errors.Is(err, port.ErrGenerativeUnavailable) {
			t.Fatalf("err = %v", err)
		}
	}
	if inner.calls != 2 {
		t.Errorf("inner calls = %d, want 2 (errors are not cached)", inner.calls)
	}
}

// slowGen counts calls safely and takes a moment, so concurrent callers
// overlap.
type slowGen struct{ calls atomic.Int32 }

func (g *slowGen) Generate(context.Context, string, string) (string, error) {
	g.calls.Add(1)
	time.Sleep(20 * time.Millisecond)
	return "summary", nil
}

func TestCacheConcurrentMissesGenerateOnce(t *testing.T) {
	inner := &slowGen{}
	dir := filepath.Join(t.TempDir(), "cache")
	c := New(inner, Dir(dir), metrics.NewRecorder())
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if text, err := c.Generate(context.Background(), "sys", "job"); err != nil || text != "summary" {
				t.Errorf("Generate = %q, %v", text, err)
			}
		})
	}
	wg.Wait()
	if n := inner.calls.Load(); n != 1 {
		t.Errorf("inner calls = %d, want 1", n)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("cache dir mode = %v (%v), want 0700", info.Mode().Perm(), err)
	}
}
