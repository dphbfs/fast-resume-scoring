// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full runtime configuration.
type Config struct {
	Jev        Jev
	Generative Generative
	Pipeline   Pipeline
	Checker    Checker
	Run        Run
}

// Run bounds one command-line run (extract, check, score).
type Run struct {
	// Deadline cancels the whole run, including queued and retrying Jev
	// calls, when it elapses.
	Deadline time.Duration // RUN_DEADLINE
}

// Jev configures the TypeSafe classifier client.
type Jev struct {
	APIKey         string        // TYPESAFE_API_KEY (required)
	BaseURL        string        // TYPESAFE_BASE_URL
	Model          string        // JEV_MODEL
	MaxConcurrency int           // JEV_MAX_CONCURRENCY
	MaxRetries     int           // JEV_MAX_RETRIES
	Timeout        time.Duration // JEV_TIMEOUT
}

// Generative configures the optional OpenAI-compatible client. An empty
// Model means "not configured".
type Generative struct {
	BaseURL        string        // OPENAI_BASE_URL
	APIKey         string        // OPENAI_API_KEY
	Model          string        // OPENAI_MODEL (no default)
	MaxConcurrency int           // GEN_MAX_CONCURRENCY
	Timeout        time.Duration // GEN_TIMEOUT
}

// Pipeline tunes the Requirement Extractor.
type Pipeline struct {
	MaxWindowWords   int // PIPELINE_MAX_WINDOW_WORDS
	SectionBatchSize int // PIPELINE_SECTION_BATCH: questions per labeling request
	// MinRequirementMass is the share of a Validation Choice's probability
	// that must fall on Candidates for a chunk to be accepted.
	MinRequirementMass float64 // PIPELINE_MIN_REQUIREMENT_MASS
	// SkipImportance leaves out the Refinement Round's Importance questions
	// (scoring mode: the Fit and Match Scores do not use Importance).
	SkipImportance bool // PIPELINE_SKIP_IMPORTANCE
	// SkipResponsibilities drops responsibilities sentences before
	// Candidate generation (scoring mode: they only yield mentioned-tier
	// Requirements, which CHECKER_SKIP_MENTIONED does not check).
	SkipResponsibilities bool // PIPELINE_SKIP_RESPONSIBILITIES
}

// Checker tunes the Resume Checker.
type Checker struct {
	// RetrievalK is how many Requirements per Evidence Unit the Retrieval
	// Round passes on; RetrievalFloor is the least probability they need.
	RetrievalK     int     // CHECKER_RETRIEVAL_K
	RetrievalFloor float64 // CHECKER_RETRIEVAL_FLOOR
	// MinEvidenceMass is the P(strong+partial+weak) a Strength Round answer
	// needs to create an Evidence Link.
	MinEvidenceMass float64 // CHECKER_MIN_EVIDENCE_MASS
	// NarrowSizes repeats the Retrieval Choice over the best NarrowSizes[i]
	// options of the previous round; empty ("none" in the env) asks one
	// round.
	NarrowSizes []int // CHECKER_NARROW_SIZES, e.g. "12,4"
	// GateThreshold > 0 adds one gate Noul per pair ("is this evidence the
	// candidate has the requirement?") and links on gate >= threshold
	// instead of MinEvidenceMass. 0 turns the gate off.
	GateThreshold float64 // CHECKER_GATE_THRESHOLD
	// SkipCappedGrading (needs the gate) asks only the gate on Skills and
	// Summary units: they are capped at weak, so the grade is never used.
	SkipCappedGrading bool // CHECKER_SKIP_CAPPED_GRADING
	// GateFirst (needs the gate) asks the gates in one request and grades
	// only the pairs that passed, in a second.
	GateFirst bool // CHECKER_GATE_FIRST
	// SkipMentioned checks only required and preferred Requirements
	// (scoring mode: the Match Score does not need the mentioned ones, which
	// are about half of all checked items).
	SkipMentioned bool // CHECKER_SKIP_MENTIONED
}

// removedSettings are experiment settings whose losing variants were
// deleted; setting one is an error rather than silently ignored.
var removedSettings = []string{
	"CHECKER_RETRIEVAL_MODE", "CHECKER_NOUL_THRESHOLD", "CHECKER_PEEL_SHORTLIST",
	"CHECKER_STRENGTH_CRITERIA", "CHECKER_VETO_THRESHOLD", "CHECKER_GATE_WORDING",
	"CHECKER_STRENGTH_MODE", "CHECKER_NARROW_STOP_P",
}

// Load reads Config from the environment, applying defaults.
func Load() (Config, error) {
	return load(os.Getenv)
}

// DefaultConfig returns every setting's default; Load starts from it and
// tests can too, so there is one source of defaults.
func DefaultConfig() Config {
	return Config{
		Jev: Jev{
			BaseURL:        "https://api.typesafe.ai",
			Model:          "jev-latest",
			MaxConcurrency: 8,
			MaxRetries:     4,
			Timeout:        30 * time.Second,
		},
		Generative: Generative{
			BaseURL:        "https://api.openai.com/v1",
			MaxConcurrency: 1,
			Timeout:        60 * time.Second,
		},
		Pipeline: Pipeline{
			MaxWindowWords:     4,
			SectionBatchSize:   60,
			MinRequirementMass: 0.7,
		},
		Checker: DefaultChecker(),
		Run:     Run{Deadline: 120 * time.Second},
	}
}

// DefaultChecker returns the Resume Checker defaults (docs/tuning.md).
func DefaultChecker() Checker {
	return Checker{
		RetrievalK:        8,
		RetrievalFloor:    0.01,
		MinEvidenceMass:   0.5,
		NarrowSizes:       []int{16},
		GateThreshold:     0.5,
		SkipCappedGrading: true,
		GateFirst:         true,
	}
}

func load(getenv func(string) string) (Config, error) {
	e := env{getenv: getenv}
	d := DefaultConfig()
	cfg := Config{
		Jev: Jev{
			APIKey:         getenv("TYPESAFE_API_KEY"),
			BaseURL:        e.str("TYPESAFE_BASE_URL", d.Jev.BaseURL),
			Model:          e.str("JEV_MODEL", d.Jev.Model),
			MaxConcurrency: e.int("JEV_MAX_CONCURRENCY", d.Jev.MaxConcurrency),
			MaxRetries:     e.int("JEV_MAX_RETRIES", d.Jev.MaxRetries),
			Timeout:        e.duration("JEV_TIMEOUT", d.Jev.Timeout),
		},
		Generative: Generative{
			BaseURL:        e.str("OPENAI_BASE_URL", d.Generative.BaseURL),
			APIKey:         getenv("OPENAI_API_KEY"),
			Model:          getenv("OPENAI_MODEL"),
			MaxConcurrency: e.int("GEN_MAX_CONCURRENCY", d.Generative.MaxConcurrency),
			Timeout:        e.duration("GEN_TIMEOUT", d.Generative.Timeout),
		},
		Pipeline: Pipeline{
			MaxWindowWords:       e.int("PIPELINE_MAX_WINDOW_WORDS", d.Pipeline.MaxWindowWords),
			SectionBatchSize:     e.int("PIPELINE_SECTION_BATCH", d.Pipeline.SectionBatchSize),
			MinRequirementMass:   e.float("PIPELINE_MIN_REQUIREMENT_MASS", d.Pipeline.MinRequirementMass),
			SkipImportance:       e.bool("PIPELINE_SKIP_IMPORTANCE", d.Pipeline.SkipImportance),
			SkipResponsibilities: e.bool("PIPELINE_SKIP_RESPONSIBILITIES", d.Pipeline.SkipResponsibilities),
		},
		Checker: Checker{
			RetrievalK:        e.int("CHECKER_RETRIEVAL_K", d.Checker.RetrievalK),
			RetrievalFloor:    e.float("CHECKER_RETRIEVAL_FLOOR", d.Checker.RetrievalFloor),
			MinEvidenceMass:   e.float("CHECKER_MIN_EVIDENCE_MASS", d.Checker.MinEvidenceMass),
			NarrowSizes:       e.ints("CHECKER_NARROW_SIZES", d.Checker.NarrowSizes),
			GateThreshold:     e.float("CHECKER_GATE_THRESHOLD", d.Checker.GateThreshold),
			SkipCappedGrading: e.bool("CHECKER_SKIP_CAPPED_GRADING", d.Checker.SkipCappedGrading),
			GateFirst:         e.bool("CHECKER_GATE_FIRST", d.Checker.GateFirst),
			SkipMentioned:     e.bool("CHECKER_SKIP_MENTIONED", d.Checker.SkipMentioned),
		},
		Run: Run{Deadline: e.duration("RUN_DEADLINE", d.Run.Deadline)},
	}
	if e.err != nil {
		return Config{}, e.err
	}
	for _, name := range removedSettings {
		if getenv(name) != "" {
			return Config{}, fmt.Errorf("config: %s was removed (experiments in docs/tuning.md); unset it", name)
		}
	}
	if cfg.Jev.APIKey == "" {
		return Config{}, fmt.Errorf("config: TYPESAFE_API_KEY is required")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks every bound: counts at least 1, durations positive,
// probabilities finite and in [0, 1], and the Checker's switch
// dependencies. It is the one validation path for loaded settings.
func (c Config) Validate() error {
	var errs []error
	atLeast := func(name string, v, lo int) {
		if v < lo {
			errs = append(errs, fmt.Errorf("%s must be at least %d, got %d", name, lo, v))
		}
	}
	positive := func(name string, d time.Duration) {
		if d <= 0 {
			errs = append(errs, fmt.Errorf("%s must be positive, got %s", name, d))
		}
	}
	prob := func(name string, p float64) {
		if math.IsNaN(p) || p < 0 || p > 1 {
			errs = append(errs, fmt.Errorf("%s must be in [0, 1], got %v", name, p))
		}
	}
	atLeast("JEV_MAX_CONCURRENCY", c.Jev.MaxConcurrency, 1)
	atLeast("JEV_MAX_RETRIES", c.Jev.MaxRetries, 0)
	positive("JEV_TIMEOUT", c.Jev.Timeout)
	atLeast("GEN_MAX_CONCURRENCY", c.Generative.MaxConcurrency, 1)
	positive("GEN_TIMEOUT", c.Generative.Timeout)
	atLeast("PIPELINE_MAX_WINDOW_WORDS", c.Pipeline.MaxWindowWords, 1)
	atLeast("PIPELINE_SECTION_BATCH", c.Pipeline.SectionBatchSize, 1)
	prob("PIPELINE_MIN_REQUIREMENT_MASS", c.Pipeline.MinRequirementMass)
	atLeast("CHECKER_RETRIEVAL_K", c.Checker.RetrievalK, 1)
	prob("CHECKER_RETRIEVAL_FLOOR", c.Checker.RetrievalFloor)
	prob("CHECKER_MIN_EVIDENCE_MASS", c.Checker.MinEvidenceMass)
	prob("CHECKER_GATE_THRESHOLD", c.Checker.GateThreshold)
	if (c.Checker.SkipCappedGrading || c.Checker.GateFirst) && c.Checker.GateThreshold <= 0 {
		errs = append(errs, fmt.Errorf("CHECKER_SKIP_CAPPED_GRADING and CHECKER_GATE_FIRST need CHECKER_GATE_THRESHOLD > 0"))
	}
	positive("RUN_DEADLINE", c.Run.Deadline)
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// env reads typed values and keeps the first parse error.
type env struct {
	getenv func(string) string
	err    error
}

func (e *env) str(key, def string) string {
	if v := e.getenv(key); v != "" {
		return v
	}
	return def
}

func (e *env) int(key string, def int) int {
	v := e.getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s: %w", key, err)
	}
	return n
}

func (e *env) bool(key string, def bool) bool {
	v := e.getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s: %w", key, err)
	}
	return b
}

// ints reads a comma-separated list of positive integers.
func (e *env) ints(key string, def []int) []int {
	v := e.getenv(key)
	if v == "" {
		return def
	}
	if v == "none" {
		return []int{}
	}
	var out []int
	for _, f := range strings.Split(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if (err != nil || n < 1) && e.err == nil {
			e.err = fmt.Errorf("config: %s: %q is not a positive integer", key, f)
		}
		out = append(out, n)
	}
	return out
}

func (e *env) float(key string, def float64) float64 {
	v := e.getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s: %w", key, err)
	}
	return f
}

func (e *env) duration(key string, def time.Duration) time.Duration {
	v := e.getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s: %w", key, err)
	}
	return d
}
