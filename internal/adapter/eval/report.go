package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/fsutil"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

// Report is one eval run.
type Report struct {
	Started  time.Time       `json:"started"`
	Duration string          `json:"duration"`
	Model    string          `json:"model"`
	Revision string          `json:"revision"`
	Pipeline config.Pipeline `json:"pipeline"`
	// Labels fingerprints the golden labels the run was scored against.
	Labels string `json:"labels"`
	// RescoredFrom is the start time of the run whose results were rescored.
	RescoredFrom string          `json:"rescored_from,omitempty"`
	Totals       Totals          `json:"totals"`
	Fixtures     []FixtureScore  `json:"fixtures"`
	Metrics      metrics.Summary `json:"metrics"`
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
	AcceptableHits int                  `json:"acceptable_hits"`
	DuplicateHits  int                  `json:"duplicate_hits"`
	Tiers          map[string]TierScore `json:"tiers"`
	// MissStages counts misses per attributed pipeline stage.
	MissStages map[string]int `json:"miss_stages,omitempty"`
	TierOrder  *float64       `json:"tier_order,omitempty"`
	GroupF1    *float64       `json:"group_f1,omitempty"`
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
		t.AcceptableHits += len(s.AcceptableHits)
		for _, mc := range s.MissCauses {
			if t.MissStages == nil {
				t.MissStages = map[string]int{}
			}
			t.MissStages[mc.Stage]++
		}
		t.DuplicateHits += len(s.DuplicateHits)
		for tier, ts := range s.Tiers {
			agg := t.Tiers[tier]
			agg.Expected += ts.Expected
			agg.Matched += ts.Matched
			agg.TierChecked += ts.TierChecked
			agg.TierCorrect += ts.TierCorrect
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
	t.PrecisionLoose = ratio(loose, t.Predicted-t.AcceptableHits-t.DuplicateHits)
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
	base := filepath.Join(dir, r.Started.Format("2006-01-02T15-04-05Z"))
	if r.RescoredFrom != "" {
		base += "-rescored"
	}
	if err := fsutil.WriteJSONAtomic(base+".json", r); err != nil {
		return "", err
	}
	if err := r.writeTraces(base + "-traces"); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := r.WriteMarkdown(&b); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(base+".md", []byte(b.String())); err != nil {
		return "", err
	}
	return base + ".md", nil
}

// writeTraces saves each fixture's trace as <dir>/<id>.json.
func (r Report) writeTraces(dir string) error {
	for _, s := range r.Fixtures {
		if s.Trace == nil {
			continue
		}
		if err := fsutil.WriteJSONAtomic(filepath.Join(dir, s.ID+".json"), s.Trace); err != nil {
			return err
		}
	}
	return nil
}

// LoadTraces reads the trace files written next to a report JSON, keyed by
// fixture ID. A report without traces yields an empty map.
func LoadTraces(reportJSON string) (map[string]*domain.Trace, error) {
	dir := strings.TrimSuffix(reportJSON, ".json") + "-traces"
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	out := map[string]*domain.Trace{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var tr domain.Trace
		if err := json.Unmarshal(raw, &tr); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = &tr
	}
	return out, nil
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
	fmt.Fprintf(&b, "Model `%s` · revision `%s` · labels `%s` · %d fixtures (%d failed) · %s\n\n",
		r.Model, r.Revision, r.Labels, t.Fixtures, t.Failed, r.Duration)
	if r.RescoredFrom != "" {
		fmt.Fprintf(&b, "Rescored offline from the run of %s against the current labels.\n\n", r.RescoredFrom)
	}
	fmt.Fprintf(&b, "Pipeline: max window %d words · section batch %d · min requirement mass %.2f\n\n",
		r.Pipeline.MaxWindowWords, r.Pipeline.SectionBatchSize, r.Pipeline.MinRequirementMass)

	b.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Recall (strict) | %s |\n", pct(t.RecallStrict))
	fmt.Fprintf(&b, "| Recall (loose) | %s |\n", pct(t.RecallLoose))
	fmt.Fprintf(&b, "| Precision (loose) | %s |\n", pct(t.PrecisionLoose))
	fmt.Fprintf(&b, "| F1 (loose) | %s |\n", pct(t.F1Loose))
	fmt.Fprintf(&b, "| Expected / predicted | %d / %d |\n", t.Expected, t.Predicted)
	fmt.Fprintf(&b, "| Filler extracted | %d |\n", t.FillerHits)
	fmt.Fprintf(&b, "| Acceptable (not scored) | %d |\n", t.AcceptableHits)
	fmt.Fprintf(&b, "| Duplicates (not scored) | %d |\n", t.DuplicateHits)
	for _, tier := range []string{"required", "preferred", "mentioned"} {
		ts := t.Tiers[tier]
		fmt.Fprintf(&b, "| Recall, %s | %s (%d/%d) |\n", tier, pct(ratio(ts.Matched, ts.Expected)), ts.Matched, ts.Expected)
	}
	for _, tier := range []string{"required", "preferred", "mentioned"} {
		if ts := t.Tiers[tier]; ts.TierChecked > 0 {
			fmt.Fprintf(&b, "| Tier accuracy, %s | %s (%d/%d) |\n", tier, pct(ratio(ts.TierCorrect, ts.TierChecked)), ts.TierCorrect, ts.TierChecked)
		}
	}
	fmt.Fprintf(&b, "| Importance tier order | %s |\n", optPct(t.TierOrder))
	fmt.Fprintf(&b, "| Alternative Group F1 | %s |\n", optPct(t.GroupF1))
	if c, ok := r.Metrics.Counters["jev.cost_micro_usd"]; ok {
		fmt.Fprintf(&b, "| Jev cost | $%.4f |\n", float64(c)/1e6)
	}
	fmt.Fprintf(&b, "| Jev calls | %d |\n\n", r.Metrics.Counters["jev.calls"])

	if len(t.MissStages) > 0 {
		b.WriteString("## Misses by stage\n\n| Stage | Misses |\n|---|---|\n")
		for _, st := range slices.Sorted(maps.Keys(t.MissStages)) {
			fmt.Fprintf(&b, "| %s | %d |\n", st, t.MissStages[st])
		}
		b.WriteString("\n")
	}

	b.WriteString("## Fixtures\n\n| Fixture | Recall (loose) | Precision | Expected | Predicted | Filler | Time |\n|---|---|---|---|---|---|---|\n")
	for _, s := range r.Fixtures {
		if s.Error != "" {
			fmt.Fprintf(&b, "| %s | error: %s | | %d | | | |\n", s.Title, s.Error, s.Expected)
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %.1fs |\n", s.Title,
			pct(ratio(s.LooseMatched, s.Expected)), pct(s.Precision()),
			s.Expected, s.Predicted, len(s.FillerHits), float64(s.DurationMS)/1000)
	}

	b.WriteString("\n## Misses and extras\n")
	for _, s := range r.Fixtures {
		if s.Error != "" {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n", s.Title)
		if len(s.MissCauses) > 0 {
			fmt.Fprintf(&b, "- **Missed (%d):**\n", len(s.Misses))
			for _, mc := range s.MissCauses {
				fmt.Fprintf(&b, "  - `%s` (%s): %s\n", mc.Expected, mc.Stage, mc.Detail)
			}
		} else {
			fmt.Fprintf(&b, "- **Missed (%d):** %s\n", len(s.Misses), quoteList(s.Misses))
		}
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
