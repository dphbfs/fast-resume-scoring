package gencache

import (
	"context"
	"errors"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
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
