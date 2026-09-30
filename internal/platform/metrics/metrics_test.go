package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestSummary(t *testing.T) {
	r := NewRecorder()
	r.Add("jev.calls", 2)
	r.Add("jev.calls", 3)
	for i := 1; i <= 20; i++ {
		r.ObserveDuration("jev.latency", time.Duration(i)*time.Millisecond)
	}

	s := r.Summary()
	if got := s.Counters["jev.calls"]; got != 5 {
		t.Errorf("jev.calls = %d, want 5", got)
	}
	d := s.Durations["jev.latency"]
	if d.Count != 20 || d.P50 != 10*time.Millisecond || d.P95 != 19*time.Millisecond || d.Max != 20*time.Millisecond {
		t.Errorf("jev.latency = %+v, want count=20 p50=10ms p95=19ms max=20ms", d)
	}

	var b strings.Builder
	if err := s.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "jev.calls") || !strings.Contains(b.String(), "p95=19ms") {
		t.Errorf("WriteText output missing metrics:\n%s", b.String())
	}
}
