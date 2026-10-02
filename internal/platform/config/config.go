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
	// RetrievalMode is single (one Choice, keep top K above the floor),
	// narrow (repeat the Choice over the best NarrowSizes[i] options of the
	// previous round), or peel (take the winner, remove it, ask again until
	// none wins or K are taken; PeelShortlist > 0 first narrows to that many
	// options with one Choice).
	//
	// noul asks one yes/no question per Requirement instead of a Choice and
	// keeps the top RetrievalK with P(yes) >= NoulThreshold.
	RetrievalMode string  // CHECKER_RETRIEVAL_MODE
	NoulThreshold float64 // CHECKER_NOUL_THRESHOLD
	NarrowSizes   []int   // CHECKER_NARROW_SIZES, e.g. "12,4"
	PeelShortlist int     // CHECKER_PEEL_SHORTLIST
	// StrengthCriteria picks the Strength Round options: v1, v2 (sharpened
	// strong/partial), v3 (v2 plus negative options), v4 (v3 plus the
	// needed_capability negative), v5 (v3 as {what, not_for, examples}), or
	// v6 (v5 with "an instance of a broad requirement is not
	// alternative_tool").
	StrengthCriteria string // CHECKER_STRENGTH_CRITERIA
	// GateThreshold > 0 adds one gate Noul per pair ("is this evidence the
	// candidate has the requirement?") and links on gate >= threshold
	// instead of MinEvidenceMass. 0 turns the gate off.
	GateThreshold float64 // CHECKER_GATE_THRESHOLD
	// VetoThreshold > 0 (needs the gate) rejects a pair the gate passed when
	// one non-evidence option of the grading Choice has at least this
	// probability. 0 turns the veto off.
	VetoThreshold float64 // CHECKER_VETO_THRESHOLD
	// GateWording is the gate Noul's yes criterion: v1 (the requirement
	// itself) or v2 (also a part, prerequisite, or broader practice).
	GateWording string // CHECKER_GATE_WORDING
	// StrengthMode grades linked pairs with the StrengthCriteria Choice
	// ("choice") or a 3-level Score ("score", needs the gate).
	StrengthMode string // CHECKER_STRENGTH_MODE
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
			RetrievalK:       e.int("CHECKER_RETRIEVAL_K", 8),
			RetrievalFloor:   e.float("CHECKER_RETRIEVAL_FLOOR", 0.01),
			MinEvidenceMass:  e.float("CHECKER_MIN_EVIDENCE_MASS", 0.5),
			RetrievalMode:    e.str("CHECKER_RETRIEVAL_MODE", "narrow"),
			NarrowSizes:      e.ints("CHECKER_NARROW_SIZES", []int{16, 8}),
			PeelShortlist:    e.int("CHECKER_PEEL_SHORTLIST", 0),
			NoulThreshold:    e.float("CHECKER_NOUL_THRESHOLD", 0.5),
			StrengthCriteria: e.str("CHECKER_STRENGTH_CRITERIA", "v5"),
			GateThreshold:    e.float("CHECKER_GATE_THRESHOLD", 0.5),
			VetoThreshold:    e.float("CHECKER_VETO_THRESHOLD", 0),
			GateWording:      e.str("CHECKER_GATE_WORDING", "v2"),
			StrengthMode:     e.str("CHECKER_STRENGTH_MODE", "choice"),
		},
	}
	if e.err != nil {
		return Config{}, e.err
	}
	switch cfg.Checker.RetrievalMode {
	case "single", "narrow", "peel", "noul":
	default:
		return Config{}, fmt.Errorf("config: CHECKER_RETRIEVAL_MODE %q: want single, narrow, peel or noul", cfg.Checker.RetrievalMode)
	}
	switch cfg.Checker.StrengthCriteria {
	case "v1", "v2", "v3", "v4", "v5", "v6":
	default:
		return Config{}, fmt.Errorf("config: CHECKER_STRENGTH_CRITERIA %q: want v1..v6", cfg.Checker.StrengthCriteria)
	}
	if cfg.Checker.VetoThreshold > 0 && (cfg.Checker.GateThreshold <= 0 || cfg.Checker.StrengthMode != "choice") {
		return Config{}, fmt.Errorf("config: CHECKER_VETO_THRESHOLD needs CHECKER_GATE_THRESHOLD > 0 and CHECKER_STRENGTH_MODE=choice")
	}
	switch cfg.Checker.StrengthMode {
	case "choice":
	case "score":
		if cfg.Checker.GateThreshold <= 0 {
			return Config{}, fmt.Errorf("config: CHECKER_STRENGTH_MODE=score needs CHECKER_GATE_THRESHOLD > 0")
		}
	default:
		return Config{}, fmt.Errorf("config: CHECKER_STRENGTH_MODE %q: want choice or score", cfg.Checker.StrengthMode)
	}
	switch cfg.Checker.GateWording {
	case "v1", "v2":
	default:
		return Config{}, fmt.Errorf("config: CHECKER_GATE_WORDING %q: want v1 or v2", cfg.Checker.GateWording)
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
