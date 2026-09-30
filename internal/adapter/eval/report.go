package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

// Report is one eval run.
type Report struct {
	Started  time.Time       `json:"started"`
	Duration string          `json:"duration"`
	Model    string          `json:"model"`
	Revision string          `json:"revision"`
	Pipeline config.Pipeline `json:"pipeline"`
	Totals   Totals          `json:"totals"`
	Fixtures []FixtureScore  `json:"fixtures"`
	Metrics  metrics.Summary `json:"metrics"`
}

// Totals are micro-averaged over fixtures that ran without error.
type Totals struct {
	Fixtures       int                  `json:"fixtures"`
	Failed         int                  `json:"failed"`
	Expected       int                  `json:"expected"`
	Predicted      int                  `json:"predicted"`
	RecallStrict   float64              `json:"recall_strict"`
	RecallLoose    float64              `json:"recall_loose"`
	PrecisionLoose float64              `json:"precision_loose"`
	F1Loose        float64              `json:"f1_loose"`
	FillerHits     int                  `json:"filler_hits"`
	Tiers          map[string]TierScore `json:"tiers"`
	TierOrder      *float64             `json:"tier_order,omitempty"`
	GroupF1        *float64             `json:"group_f1,omitempty"`
}

func totals(scores []FixtureScore) Totals {
	t := Totals{Fixtures: len(scores), Tiers: map[string]TierScore{}}
	strict, loose := 0, 0
	var order, groups []float64
	for _, s := range scores {
		if s.Error != "" {
			t.Failed++
			continue
		}
		t.Expected += s.Expected
		t.Predicted += s.Predicted
		strict += s.StrictMatched
		loose += s.LooseMatched
		t.FillerHits += len(s.FillerHits)
		for tier, ts := range s.Tiers {
			agg := t.Tiers[tier]
			agg.Expected += ts.Expected
			agg.Matched += ts.Matched
			t.Tiers[tier] = agg
		}
		if s.TierOrder != nil {
			order = append(order, *s.TierOrder)
		}
		if s.GroupF1 != nil {
			groups = append(groups, *s.GroupF1)
		}
	}
	t.RecallStrict = ratio(strict, t.Expected)
	t.RecallLoose = ratio(loose, t.Expected)
	t.PrecisionLoose = ratio(loose, t.Predicted)
	if p, r := t.PrecisionLoose, t.RecallLoose; p+r > 0 {
		t.F1Loose = 2 * p * r / (p + r)
	}
	t.TierOrder = mean(order)
	t.GroupF1 = mean(groups)
	return t
}

func mean(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	m := s / float64(len(xs))
	return &m
}

// Write saves the report as <dir>/<timestamp>.json and .md and returns the
// Markdown path.
func (r Report) Write(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Join(dir, r.Started.Format("2006-01-02T15-04-05Z"))
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(base+".json", append(raw, '\n'), 0o644); err != nil {
		return "", err
	}
	f, err := os.Create(base + ".md")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := r.WriteMarkdown(f); err != nil {
		return "", err
	}
	return base + ".md", nil
}

func pct(v float64) string { return fmt.Sprintf("%.1f%%", 100*v) }

func optPct(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return pct(*v)
}

// WriteMarkdown renders a summary and the per-fixture detail.
func (r Report) WriteMarkdown(w io.Writer) error {
	t := r.Totals
	var b strings.Builder
	fmt.Fprintf(&b, "# Eval %s\n\n", r.Started.Format(time.RFC3339))
	fmt.Fprintf(&b, "Model `%s` · revision `%s` · %d fixtures (%d failed) · %s\n\n",
		r.Model, r.Revision, t.Fixtures, t.Failed, r.Duration)
	fmt.Fprintf(&b, "Pipeline: max window %d words · section batch %d · min requirement mass %.2f\n\n",
		r.Pipeline.MaxWindowWords, r.Pipeline.SectionBatchSize, r.Pipeline.MinRequirementMass)

	b.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Recall (strict) | %s |\n", pct(t.RecallStrict))
	fmt.Fprintf(&b, "| Recall (loose) | %s |\n", pct(t.RecallLoose))
	fmt.Fprintf(&b, "| Precision (loose) | %s |\n", pct(t.PrecisionLoose))
	fmt.Fprintf(&b, "| F1 (loose) | %s |\n", pct(t.F1Loose))
	fmt.Fprintf(&b, "| Expected / predicted | %d / %d |\n", t.Expected, t.Predicted)
	fmt.Fprintf(&b, "| Filler extracted | %d |\n", t.FillerHits)
	for _, tier := range []string{"required", "preferred", "mentioned"} {
		ts := t.Tiers[tier]
		fmt.Fprintf(&b, "| Recall, %s | %s (%d/%d) |\n", tier, pct(ratio(ts.Matched, ts.Expected)), ts.Matched, ts.Expected)
	}
	fmt.Fprintf(&b, "| Importance tier order | %s |\n", optPct(t.TierOrder))
	fmt.Fprintf(&b, "| Alternative Group F1 | %s |\n", optPct(t.GroupF1))
	if c, ok := r.Metrics.Counters["jev.cost_micro_usd"]; ok {
		fmt.Fprintf(&b, "| Jev cost | $%.4f |\n", float64(c)/1e6)
	}
	fmt.Fprintf(&b, "| Jev calls | %d |\n\n", r.Metrics.Counters["jev.calls"])

	b.WriteString("## Fixtures\n\n| Fixture | Recall (loose) | Precision | Expected | Predicted | Filler | Time |\n|---|---|---|---|---|---|---|\n")
	for _, s := range r.Fixtures {
		if s.Error != "" {
			fmt.Fprintf(&b, "| %s | error: %s | | %d | | | |\n", s.Title, s.Error, s.Expected)
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %.1fs |\n", s.Title,
			pct(ratio(s.LooseMatched, s.Expected)), pct(ratio(s.LooseMatched, s.Predicted)),
			s.Expected, s.Predicted, len(s.FillerHits), float64(s.DurationMS)/1000)
	}

	b.WriteString("\n## Misses and extras\n")
	for _, s := range r.Fixtures {
		if s.Error != "" {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n", s.Title)
		fmt.Fprintf(&b, "- **Missed (%d):** %s\n", len(s.Misses), quoteList(s.Misses))
		fmt.Fprintf(&b, "- **Extra (%d):** %s\n", len(s.Extras), quoteList(s.Extras))
		if len(s.FillerHits) > 0 {
			fmt.Fprintf(&b, "- **Filler extracted:** %s\n", quoteList(s.FillerHits))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func quoteList(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	q := slices.Clone(xs)
	for i, x := range q {
		q[i] = "`" + x + "`"
	}
	return strings.Join(q, ", ")
}
