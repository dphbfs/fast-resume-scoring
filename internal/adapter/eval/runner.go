package eval

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// Runner runs the extractor over fixtures and scores the results.
type Runner struct {
	extractor port.RequirementExtractor
	recorder  *metrics.Recorder
	log       *slog.Logger
}

// NewRunner builds a Runner.
func NewRunner(extractor port.RequirementExtractor, recorder *metrics.Recorder, log *slog.Logger) *Runner {
	return &Runner{extractor: extractor, recorder: recorder, log: log.With("component", "eval")}
}

// Run extracts and scores every fixture, at most parallel at a time. A
// fixture that fails is reported with its error; the run continues.
func (r *Runner) Run(ctx context.Context, fixtures []Fixture, parallel int) Report {
	start := time.Now()
	scores := make([]FixtureScore, len(fixtures))
	var mu sync.Mutex
	model := ""

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(1, parallel))
	for i, f := range fixtures {
		g.Go(func() error {
			t0 := time.Now()
			res, err := r.extractor.Extract(gctx, f.JD)
			var s FixtureScore
			if err != nil {
				r.log.ErrorContext(gctx, "fixture failed", "id", f.ID, "error", err)
				s = FixtureScore{ID: f.ID, Expected: len(f.Expected.Requirements), Error: err.Error()}
			} else {
				s = Score(f.Expected, res)
				mu.Lock()
				model = res.Model
				mu.Unlock()
			}
			s.ID, s.Title, s.DurationMS = f.ID, f.JD.Title, time.Since(t0).Milliseconds()
			scores[i] = s
			r.log.InfoContext(gctx, "fixture scored", "id", f.ID,
				"recall_loose", ratio(s.LooseMatched, s.Expected), "predicted", s.Predicted)
			return nil
		})
	}
	_ = g.Wait()

	return Report{
		Started:  start.UTC(),
		Duration: time.Since(start).Round(time.Millisecond).String(),
		Model:    model,
		Revision: revision(),
		Totals:   totals(scores),
		Fixtures: scores,
		Metrics:  r.recorder.Summary(),
	}
}

// revision is the VCS revision stamped into the binary by `go build`.
func revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "unknown", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value[:min(12, len(s.Value))]
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
