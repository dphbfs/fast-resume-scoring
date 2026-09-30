// Package metrics provides the CLI implementation of port.Metrics: it keeps
// counters and durations in memory and renders a run summary at the end.
package metrics

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// Recorder collects metrics for one run. It is safe for concurrent use.
type Recorder struct {
	mu        sync.Mutex
	counters  map[string]int64
	durations map[string][]time.Duration
}

var _ port.Metrics = (*Recorder)(nil)

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{
		counters:  map[string]int64{},
		durations: map[string][]time.Duration{},
	}
}

// Add increments the counter name by delta.
func (r *Recorder) Add(name string, delta int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[name] += delta
}

// ObserveDuration records one duration sample under name.
func (r *Recorder) ObserveDuration(name string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.durations[name] = append(r.durations[name], d)
}

// Summary is a point-in-time view of the recorded metrics.
type Summary struct {
	Counters  map[string]int64           `json:"counters"`
	Durations map[string]DurationSummary `json:"durations"`
}

// DurationSummary aggregates the samples of one duration metric.
type DurationSummary struct {
	Count int           `json:"count"`
	P50   time.Duration `json:"p50"`
	P95   time.Duration `json:"p95"`
	Max   time.Duration `json:"max"`
}

// Summary returns the aggregated metrics recorded so far.
func (r *Recorder) Summary() Summary {
	r.mu.Lock()
	defer r.mu.Unlock()

	s := Summary{
		Counters:  maps.Clone(r.counters),
		Durations: make(map[string]DurationSummary, len(r.durations)),
	}
	for name, samples := range r.durations {
		sorted := slices.Clone(samples)
		slices.Sort(sorted)
		s.Durations[name] = DurationSummary{
			Count: len(sorted),
			P50:   percentile(sorted, 0.50),
			P95:   percentile(sorted, 0.95),
			Max:   sorted[len(sorted)-1],
		}
	}
	return s
}

// WriteText writes the summary as aligned lines, sorted by name.
func (s Summary) WriteText(w io.Writer) error {
	for _, name := range slices.Sorted(maps.Keys(s.Counters)) {
		if _, err := fmt.Fprintf(w, "%-40s %d\n", name, s.Counters[name]); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(s.Durations)) {
		d := s.Durations[name]
		if _, err := fmt.Fprintf(w, "%-40s n=%d p50=%s p95=%s max=%s\n",
			name, d.Count, d.P50, d.P95, d.Max); err != nil {
			return err
		}
	}
	return nil
}

// percentile uses the nearest-rank method on sorted, non-empty samples.
func percentile(sorted []time.Duration, p float64) time.Duration {
	idx := int(p*float64(len(sorted))+0.5) - 1
	idx = max(0, min(idx, len(sorted)-1))
	return sorted[idx]
}
