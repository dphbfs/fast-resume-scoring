package eval

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-tailoring/internal/app"
	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/fsutil"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

// E2E sets: which reference pairs to run.
const (
	// E2ESubset is the fixed 30-pair tuning subset, scored by today's
	// reference scorer (testdata/reference/current.json).
	E2ESubset = "subset"
	// E2ECurrent is every pair with a current reference score.
	E2ECurrent = "current"
	// E2EAll is every reference pair; pairs without a current score are
	// compared on ranking only (against their saved score).
	E2EAll = "all"
)

// E2EPair is one (Job Description, Resume) pair with its reference score.
type E2EPair struct {
	ID     string
	JD     domain.JobDescription
	Resume domain.Resume
	// Reference is today's reference score, nil when the pair was not
	// rescored (set "all").
	Reference *float64
	// Saved is the reference score saved when the pair was first scored,
	// nil for sets scored only once (the final set).
	Saved *int
}

// LoadE2E reads the reference pairs in dir (testdata/reference, or
// testdata/final for the sealed final set) for set,
// optionally limited to ID prefixes. Resume paths in pairs.json are
// relative to root (the repository root).
func LoadE2E(dir, root, set string, prefixes []string) ([]E2EPair, error) {
	var pairs struct {
		Pairs []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Resume string `json:"resume"`
			JD     string `json:"jd"`
			Score  *int   `json:"score"`
			Subset bool   `json:"subset"`
		} `json:"pairs"`
	}
	var current struct {
		Pairs []struct {
			Pair  string  `json:"pair"`
			Score float64 `json:"score"`
		} `json:"pairs"`
	}
	if err := readJSON(filepath.Join(dir, "pairs.json"), &pairs); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "current.json"), &current); err != nil {
		return nil, err
	}
	ref := make(map[string]float64, len(current.Pairs))
	for _, p := range current.Pairs {
		ref[p.Pair] = p.Score
	}

	resumes := map[string]domain.Resume{}
	var out []E2EPair
	for _, p := range pairs.Pairs {
		r, rescored := ref[p.ID]
		switch {
		case set == E2ESubset && !p.Subset, set == E2ECurrent && !rescored:
			continue
		case set != E2ESubset && set != E2ECurrent && set != E2EAll:
			return nil, fmt.Errorf("e2e set %q: want %s, %s or %s", set, E2ESubset, E2ECurrent, E2EAll)
		}
		if len(prefixes) > 0 && !slices.ContainsFunc(prefixes, func(pre string) bool { return strings.HasPrefix(p.ID, pre) }) {
			continue
		}
		if set == E2ESubset && !rescored {
			return nil, fmt.Errorf("subset pair %s has no current reference score", p.ID)
		}
		text, err := os.ReadFile(filepath.Join(dir, p.JD))
		if err != nil {
			return nil, err
		}
		resume, ok := resumes[p.Resume]
		if !ok {
			raw, err := os.ReadFile(filepath.Join(root, p.Resume))
			if err != nil {
				return nil, err
			}
			resume = domain.Resume{Text: string(raw)}
			resumes[p.Resume] = resume
		}
		pair := E2EPair{ID: p.ID, JD: domain.JobDescription{Title: p.Title, Text: string(text)}, Resume: resume, Saved: p.Score}
		if rescored {
			pair.Reference = &r
		}
		out = append(out, pair)
	}
	if len(out) == 0 {
		return nil, errors.New("e2e: no pairs selected")
	}
	return out, nil
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// E2ERunner scores each pair with the Holistic Round (the Match Score) and
// compares it with the reference score; with the Fit pipeline on, it also
// extracts each pair's Requirements, checks the Resume against them, and
// compares the Fit Score.
type E2ERunner struct {
	extractor port.RequirementExtractor
	checker   port.ResumeChecker
	holistic  port.HolisticJudge
	recorder  *metrics.Recorder
	log       *slog.Logger
	jev       config.Jev
	pipeline  config.Pipeline
	cfg       config.Checker
	tuning    *tuning.Tuning
}

// NewE2ERunner builds an E2ERunner. The configs are recorded in each report
// and key the extraction cache.
func NewE2ERunner(extractor port.RequirementExtractor, checker port.ResumeChecker, holistic port.HolisticJudge, recorder *metrics.Recorder,
	log *slog.Logger, jev config.Jev, pipeline config.Pipeline, cfg config.Checker, t *tuning.Tuning) *E2ERunner {
	return &E2ERunner{extractor: extractor, checker: checker, holistic: holistic, recorder: recorder, log: log.With("component", "eval"),
		jev: jev, pipeline: pipeline, cfg: cfg, tuning: t}
}

// E2EScore is one pair's outcome.
type E2EScore struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Reference *float64 `json:"reference,omitempty"`
	Saved     *int     `json:"saved,omitempty"`
	// Match is the Match Score, from the Holistic Round (nil on error).
	Match    *int             `json:"match,omitempty"`
	Holistic *domain.Holistic `json:"holistic,omitempty"`
	// Fit is the Fit Score; nil when the Fit pipeline did not run or had
	// nothing to score.
	Fit          *int                   `json:"fit,omitempty"`
	Requirements int                    `json:"requirements,omitempty"`
	Cached       bool                   `json:"extraction_cached,omitempty"`
	HolisticMS   int64                  `json:"holistic_ms"`
	ExtractMS    int64                  `json:"extract_ms,omitempty"`
	CheckMS      int64                  `json:"check_ms,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Result       *domain.Result         `json:"-"`
	Coverage     *domain.CoverageResult `json:"-"`
	Trace        *domain.CheckTrace     `json:"-"`
}

// E2EReport is one end-to-end eval run.
type E2EReport struct {
	Started  time.Time       `json:"started"`
	Duration string          `json:"duration"`
	Model    string          `json:"model"`
	Revision string          `json:"revision"`
	Tuning   string          `json:"tuning,omitempty"`
	Set      string          `json:"set"`
	Dir      string          `json:"dir,omitempty"`
	WithFit  bool            `json:"with_fit,omitempty"`
	Pipeline config.Pipeline `json:"pipeline"`
	Checker  config.Checker  `json:"checker"`
	Totals   E2ETotals       `json:"totals"`
	Pairs    []E2EScore      `json:"pairs"`
	Metrics  metrics.Summary `json:"metrics"`
}

// E2ETotals compare Match Scores (and Fit Scores, when the Fit pipeline
// ran) with reference scores over the pairs that produced both.
type E2ETotals struct {
	Pairs  int `json:"pairs"`
	Failed int `json:"failed"`
	// Agreement is the Match Score against today's reference scores.
	Agreement
	// Fit is the same comparison for the Fit Score, when it ran.
	Fit *Agreement `json:"fit,omitempty"`
	// Ranking of the Match Score against the saved scores.
	SavedPairs int     `json:"saved_pairs"`
	SavedTauB  float64 `json:"saved_tau_b"`
	// Cold pairs ran extraction (not served from the cache).
	ColdPairs      int     `json:"cold_pairs,omitempty"`
	JevCostPerPair float64 `json:"jev_cost_per_pair_usd"`
	MeanMS         int64   `json:"mean_ms"`
}

// Run scores every pair, at most parallel at a time, with the Holistic
// Round. With withFit, each pair is also extracted (or read from cacheDir
// when it is set) and checked, alongside the Holistic Round. A failed pair
// is reported with its error; the run continues.
func (r *E2ERunner) Run(ctx context.Context, pairs []E2EPair, parallel int, cacheDir string, withFit bool) E2EReport {
	start := time.Now()
	scores := make([]E2EScore, len(pairs))
	var mu sync.Mutex
	model := ""

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(1, parallel))
	for i, p := range pairs {
		g.Go(func() error {
			s := E2EScore{ID: p.ID, Title: p.JD.Title, Reference: p.Reference, Saved: p.Saved}
			if err := r.score(gctx, p, cacheDir, withFit, &s); err != nil {
				r.log.ErrorContext(gctx, "pair failed", "id", p.ID, "error", err)
				s.Error = err.Error()
			} else {
				mu.Lock()
				model = s.Holistic.Model
				mu.Unlock()
			}
			scores[i] = s
			r.log.InfoContext(gctx, "pair scored", "id", p.ID, "match", s.Match, "fit", s.Fit, "reference", p.Reference)
			return nil
		})
	}
	_ = g.Wait()

	summary := r.recorder.Summary()
	return E2EReport{
		Started:  start.UTC(),
		Duration: time.Since(start).Round(time.Millisecond).String(),
		Model:    model,
		Revision: revision(),
		Tuning:   r.tuning.Hash,
		WithFit:  withFit,
		Pipeline: r.pipeline,
		Checker:  r.cfg,
		Totals:   e2eTotals(scores, summary.Counters["jev.cost_micro_usd"]),
		Pairs:    scores,
		Metrics:  summary,
	}
}

func (r *E2ERunner) score(ctx context.Context, p E2EPair, cacheDir string, withFit bool, s *E2EScore) error {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		t0 := time.Now()
		h, err := r.holistic.Judge(gctx, p.JD, p.Resume)
		if err != nil {
			return fmt.Errorf("holistic: %w", err)
		}
		s.HolisticMS = time.Since(t0).Milliseconds()
		s.Holistic = &h
		return nil
	})
	g.Go(func() error {
		if !withFit {
			return nil
		}
		t0 := time.Now()
		res, cached, err := r.extract(gctx, p.JD, cacheDir)
		if err != nil {
			return fmt.Errorf("extract: %w", err)
		}
		s.ExtractMS, s.Cached, s.Requirements = time.Since(t0).Milliseconds(), cached, len(res.Requirements)
		t0 = time.Now()
		cov, tr, err := r.checker.Check(gctx, res, p.Resume)
		if err != nil {
			return fmt.Errorf("check: %w", err)
		}
		s.CheckMS = time.Since(t0).Milliseconds()
		s.Fit, s.Result, s.Coverage, s.Trace = cov.Fit.Score, &res, &cov, &tr
		return nil
	})
	if err := g.Wait(); err != nil {
		return err
	}
	m := domain.MatchScore(*s.Holistic, r.tuning.MatchWeights())
	s.Match = &m
	return nil
}

// extract returns the Job Description's Requirements, from cacheDir when a
// result for the same text, model, and pipeline settings is there.
func (r *E2ERunner) extract(ctx context.Context, jd domain.JobDescription, cacheDir string) (domain.Result, bool, error) {
	var path string
	if cacheDir != "" {
		key, err := json.Marshal(struct {
			Version  string
			Text     string
			Model    string
			Pipeline config.Pipeline
			Tuning   string
		}{app.ExtractorVersion, jd.Text, r.jev.Model, r.pipeline, r.tuning.Hash})
		if err != nil {
			return domain.Result{}, false, err
		}
		sum := sha256.Sum256(key)
		path = filepath.Join(cacheDir, hex.EncodeToString(sum[:8])+".json")
		var res domain.Result
		if err := readJSON(path, &res); err == nil {
			return res, true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return domain.Result{}, false, err
		}
	}
	res, _, err := r.extractor.Extract(ctx, jd)
	if err != nil || path == "" {
		return res, false, err
	}
	if err := fsutil.WriteJSONAtomic(path, res); err != nil {
		return domain.Result{}, false, err
	}
	return res, false, nil
}

// Agreement compares one score with the reference scores.
type Agreement struct {
	Scored   int     `json:"scored"`
	MAE      float64 `json:"mae"`
	Bias     float64 `json:"bias"`
	Within5  float64 `json:"within_5"`
	Within10 float64 `json:"within_10"`
	MaxError float64 `json:"max_error"`
	Pearson  float64 `json:"pearson"`
	TauB     float64 `json:"tau_b"`
}

// agree compares pred with ref; the zero Agreement when there are none.
func agree(pred, ref []float64) Agreement {
	a := Agreement{Scored: len(pred)}
	if len(pred) == 0 {
		return a
	}
	var within5, within10 int
	for i := range pred {
		d := pred[i] - ref[i]
		a.Bias += d
		a.MAE += math.Abs(d)
		a.MaxError = max(a.MaxError, math.Abs(d))
		if math.Abs(d) <= 5 {
			within5++
		}
		if math.Abs(d) <= 10 {
			within10++
		}
	}
	n := float64(len(pred))
	a.Bias, a.MAE = a.Bias/n, a.MAE/n
	a.Within5, a.Within10 = float64(within5)/n, float64(within10)/n
	a.Pearson = pearson(pred, ref)
	a.TauB = kendallTauB(pred, ref)
	return a
}

func e2eTotals(scores []E2EScore, costMicro int64) E2ETotals {
	t := E2ETotals{Pairs: len(scores)}
	var match, ref, saved, savedMatch, fit, fitRef []float64
	var ms int64
	fitRan := false
	for _, s := range scores {
		if s.Error != "" {
			t.Failed++
			continue
		}
		// Holistic and the Fit pipeline run side by side.
		ms += max(s.HolisticMS, s.ExtractMS+s.CheckMS)
		if s.ExtractMS > 0 || s.CheckMS > 0 || s.Fit != nil {
			fitRan = true
			if !s.Cached {
				t.ColdPairs++
			}
		}
		if s.Match == nil {
			continue
		}
		if s.Saved != nil {
			savedMatch = append(savedMatch, float64(*s.Match))
			saved = append(saved, float64(*s.Saved))
		}
		if s.Reference == nil {
			continue
		}
		match = append(match, float64(*s.Match))
		ref = append(ref, *s.Reference)
		if s.Fit != nil {
			fit = append(fit, float64(*s.Fit))
			fitRef = append(fitRef, *s.Reference)
		}
	}
	if ran := t.Pairs - t.Failed; ran > 0 {
		t.MeanMS = ms / int64(ran)
		t.JevCostPerPair = float64(costMicro) / 1e6 / float64(ran)
	}
	t.SavedPairs = len(savedMatch)
	t.SavedTauB = kendallTauB(savedMatch, saved)
	t.Agreement = agree(match, ref)
	if fitRan {
		f := agree(fit, fitRef)
		t.Fit = &f
	}
	return t
}

// pearson is the correlation of x and y; 0 when either is constant.
func pearson(x, y []float64) float64 {
	n := float64(len(x))
	var mx, my float64
	for i := range x {
		mx += x[i] / n
		my += y[i] / n
	}
	var cov, vx, vy float64
	for i := range x {
		cov += (x[i] - mx) * (y[i] - my)
		vx += (x[i] - mx) * (x[i] - mx)
		vy += (y[i] - my) * (y[i] - my)
	}
	if vx == 0 || vy == 0 {
		return 0
	}
	return cov / math.Sqrt(vx*vy)
}

// kendallTauB is Kendall's rank correlation with the tie correction; 0
// when either side has no untied pair.
func kendallTauB(x, y []float64) float64 {
	var concordant, discordant, tiesX, tiesY float64
	for i := range x {
		for j := i + 1; j < len(x); j++ {
			dx, dy := cmp.Compare(x[i], x[j]), cmp.Compare(y[i], y[j])
			switch {
			case dx == 0 && dy == 0:
			case dx == 0:
				tiesX++
			case dy == 0:
				tiesY++
			case dx == dy:
				concordant++
			default:
				discordant++
			}
		}
	}
	d := math.Sqrt((concordant + discordant + tiesX) * (concordant + discordant + tiesY))
	if d == 0 {
		return 0
	}
	return (concordant - discordant) / d
}

// Write saves the report as <dir>/<timestamp>.json and .md, with each
// pair's extraction result and check trace in <timestamp>-traces/, and
// returns the Markdown path.
func (r E2EReport) Write(dir string) (string, error) {
	base := filepath.Join(dir, r.Started.Format("2006-01-02T15-04-05Z"))
	if err := fsutil.WriteJSONAtomic(base+".json", r); err != nil {
		return "", err
	}
	for _, s := range r.Pairs {
		if s.Trace == nil {
			continue
		}
		v := struct {
			Result   *domain.Result         `json:"result"`
			Coverage *domain.CoverageResult `json:"coverage"`
			Trace    *domain.CheckTrace     `json:"trace"`
			Holistic *domain.Holistic       `json:"holistic,omitempty"`
		}{s.Result, s.Coverage, s.Trace, s.Holistic}
		if err := fsutil.WriteJSONAtomic(filepath.Join(base+"-traces", s.ID+".json"), v); err != nil {
			return "", err
		}
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

// WriteMarkdown renders the agreement summary and every pair, largest
// disagreement first.
func (r E2EReport) WriteMarkdown(w io.Writer) error {
	t := r.Totals
	var b strings.Builder
	fmt.Fprintf(&b, "# End-to-end eval %s\n\n", r.Started.Format(time.RFC3339))
	fmt.Fprintf(&b, "Model `%s` · revision `%s` · set `%s` · %d pairs (%d failed) · %s\n\n",
		r.Model, r.Revision, r.Set, t.Pairs, t.Failed, r.Duration)
	b.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Match Score MAE vs reference** | **%.1f** (%d pairs) |\n", t.MAE, t.Scored)
	fmt.Fprintf(&b, "| Bias (Match - reference) | %+.1f |\n", t.Bias)
	fmt.Fprintf(&b, "| Within ±5 / ±10 | %.0f%% / %.0f%% |\n", 100*t.Within5, 100*t.Within10)
	fmt.Fprintf(&b, "| Max error | %.0f |\n", t.MaxError)
	fmt.Fprintf(&b, "| Pearson / Kendall τ-b vs reference | %.2f / %.2f |\n", t.Pearson, t.TauB)
	if t.SavedPairs > 0 {
		fmt.Fprintf(&b, "| Kendall τ-b vs saved scores | %.2f (%d pairs) |\n", t.SavedTauB, t.SavedPairs)
	}
	if f := t.Fit; f != nil {
		fmt.Fprintf(&b, "| Fit Score MAE / bias / τ-b | %.1f / %+.1f / %.2f (%d pairs, %d cold extractions) |\n",
			f.MAE, f.Bias, f.TauB, f.Scored, t.ColdPairs)
	}
	fmt.Fprintf(&b, "| Jev cost per pair | $%.4f |\n", t.JevCostPerPair)
	fmt.Fprintf(&b, "| Mean time per pair | %.1fs |\n\n", float64(t.MeanMS)/1000)

	pairs := slices.Clone(r.Pairs)
	errOf := func(s E2EScore) float64 {
		if s.Match == nil || s.Reference == nil {
			return -1
		}
		return math.Abs(float64(*s.Match) - *s.Reference)
	}
	slices.SortStableFunc(pairs, func(a, b E2EScore) int { return cmp.Compare(errOf(b), errOf(a)) })
	b.WriteString("| Pair | Match | Reference | Saved | Role match | Experience short | Blocker | Fit | Time |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, s := range pairs {
		ref, saved := "–", "–"
		if s.Reference != nil {
			ref = fmt.Sprintf("%.0f", *s.Reference)
		}
		if s.Saved != nil {
			saved = strconv.Itoa(*s.Saved)
		}
		match := fitText(s.Match)
		if s.Error != "" {
			match = "error: " + cell(s.Error)
		}
		role, short, blocker := "–", "–", "–"
		if h := s.Holistic; h != nil {
			role, short, blocker = fmt.Sprintf("%.2f", h.RoleMatch), fmt.Sprintf("%.2f", h.ExperienceShort), fmt.Sprintf("%.2f", h.Blocker)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %.1fs |\n", cell(s.Title), match, ref, saved, role, short, blocker,
			fitText(s.Fit), float64(max(s.HolisticMS, s.ExtractMS+s.CheckMS))/1000)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
