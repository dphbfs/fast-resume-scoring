// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
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
	// options of the previous round; empty asks one round.
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

func load(getenv func(string) string) (Config, error) {
	e := env{getenv: getenv}
	cfg := Config{
		Jev: Jev{
			APIKey:         getenv("TYPESAFE_API_KEY"),
			BaseURL:        e.str("TYPESAFE_BASE_URL", "https://api.typesafe.ai"),
			Model:          e.str("JEV_MODEL", "jev-latest"),
			MaxConcurrency: e.int("JEV_MAX_CONCURRENCY", 8),
			MaxRetries:     e.int("JEV_MAX_RETRIES", 4),
			Timeout:        e.duration("JEV_TIMEOUT", 30*time.Second),
		},
		Generative: Generative{
			BaseURL:        e.str("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			APIKey:         getenv("OPENAI_API_KEY"),
			Model:          getenv("OPENAI_MODEL"),
			MaxConcurrency: e.int("GEN_MAX_CONCURRENCY", 1),
			Timeout:        e.duration("GEN_TIMEOUT", 60*time.Second),
		},
		Pipeline: Pipeline{
			MaxWindowWords:     e.int("PIPELINE_MAX_WINDOW_WORDS", 4),
			SectionBatchSize:   e.int("PIPELINE_SECTION_BATCH", 60),
			MinRequirementMass: e.float("PIPELINE_MIN_REQUIREMENT_MASS", 0.7),
		},
		Checker: Checker{
			RetrievalK:        e.int("CHECKER_RETRIEVAL_K", 8),
			RetrievalFloor:    e.float("CHECKER_RETRIEVAL_FLOOR", 0.01),
			MinEvidenceMass:   e.float("CHECKER_MIN_EVIDENCE_MASS", 0.5),
			NarrowSizes:       e.ints("CHECKER_NARROW_SIZES", []int{16}),
			GateThreshold:     e.float("CHECKER_GATE_THRESHOLD", 0.5),
			SkipCappedGrading: e.bool("CHECKER_SKIP_CAPPED_GRADING", true),
			GateFirst:         e.bool("CHECKER_GATE_FIRST", true),
		},
	}
	if e.err != nil {
		return Config{}, e.err
	}
	for _, name := range removedSettings {
		if getenv(name) != "" {
			return Config{}, fmt.Errorf("config: %s was removed (experiments in docs/tuning.md); unset it", name)
		}
	}
	if (cfg.Checker.SkipCappedGrading || cfg.Checker.GateFirst) && cfg.Checker.GateThreshold <= 0 {
		return Config{}, fmt.Errorf("config: CHECKER_SKIP_CAPPED_GRADING and CHECKER_GATE_FIRST need CHECKER_GATE_THRESHOLD > 0")
	}
	if cfg.Jev.APIKey == "" {
		return Config{}, fmt.Errorf("config: TYPESAFE_API_KEY is required")
	}
	return cfg, nil
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
