// Package tuning loads tuning.yaml: every prompt the app sends to Jev or
// the generative model, and every weight it scores with. The binaries embed
// the file; TUNING_FILE points at an edited copy instead.
package tuning

import (
	"bytes"
	"crypto/sha256"
	_ "embed" // the default tuning file
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
)

//go:embed tuning.yaml
var embedded []byte

// Version is the tuning file format this code reads.
const Version = 1

// Tuning is the parsed tuning file.
type Tuning struct {
	Version    int        `yaml:"version"`
	MatchScore MatchScore `yaml:"match_score"`
	FitScore   FitScore   `yaml:"fit_score"`
	Holistic   Holistic   `yaml:"holistic"`
	Extractor  Extractor  `yaml:"extractor"`
	Checker    Checker    `yaml:"checker"`

	// Source is "embedded" or the file path; Hash is the first 12 hex
	// digits of the file's SHA-256. Reports record both.
	Source string `yaml:"-"`
	Hash   string `yaml:"-"`
}

// MatchScore holds the Match Score weights (ADR 0004).
type MatchScore struct {
	Intercept       float64 `yaml:"intercept"`
	RoleMatch       float64 `yaml:"role_match"`
	ExperienceShort float64 `yaml:"experience_short"`
}

// FitScore holds the Fit Score credits and Tier weights (ADR 0002).
type FitScore struct {
	Credit     map[string]float64 `yaml:"credit"`
	TierWeight map[string]float64 `yaml:"tier_weight"`
}

// ScoreQuestion is a Score question with its levels, lowest first.
type ScoreQuestion struct {
	Question string   `yaml:"question"`
	Levels   []string `yaml:"levels"`
}

// NoulQuestion is a yes/no question with what each answer means.
type NoulQuestion struct {
	Question string `yaml:"question"`
	IfTrue   string `yaml:"if_true"`
	IfFalse  string `yaml:"if_false"`
}

// ChoiceQuestion is a Choice question with described options.
type ChoiceQuestion struct {
	Question string            `yaml:"question"`
	Options  map[string]string `yaml:"options"`
}

// ReasonQuestion is a Choice question whose fixed options come from the
// data (Candidates, other Requirements) plus these reason options.
type ReasonQuestion struct {
	Question string            `yaml:"question"`
	Reasons  map[string]string `yaml:"reasons"`
}

// Holistic holds the Holistic Round questions.
type Holistic struct {
	RoleMatch       ScoreQuestion `yaml:"role_match"`
	ExperienceShort NoulQuestion  `yaml:"experience_short"`
	Blocker         NoulQuestion  `yaml:"blocker"`
}

// Extractor holds the Requirement Extractor prompts.
type Extractor struct {
	Section    ChoiceQuestion `yaml:"section"`
	JobSummary JobSummary     `yaml:"job_summary"`
	Validation Validation     `yaml:"validation"`
	Refinement Refinement     `yaml:"refinement"`
}

// JobSummary is the generative model's system prompt.
type JobSummary struct {
	SystemPrompt string `yaml:"system_prompt"`
}

// Validation is the Validation Round question.
type Validation struct {
	Question      string            `yaml:"question"`
	RejectOptions map[string]string `yaml:"reject_options"`
}

// Refinement holds the Refinement Round questions.
type Refinement struct {
	Filler      Filler         `yaml:"filler"`
	Duplicate   ReasonQuestion `yaml:"duplicate"`
	Alternative ReasonQuestion `yaml:"alternative"`
	Importance  ScoreQuestion  `yaml:"importance"`
}

// Filler is the Filler question: Keep describes the keep option.
type Filler struct {
	Question string            `yaml:"question"`
	Keep     string            `yaml:"keep"`
	Reasons  map[string]string `yaml:"reasons"`
}

// Checker holds the Resume Checker prompts.
type Checker struct {
	Retrieval Retrieval `yaml:"retrieval"`
	Gate      Gate      `yaml:"gate"`
	Strength  Strength  `yaml:"strength"`
}

// Retrieval is the Retrieval Round question.
type Retrieval struct {
	Question      string `yaml:"question"`
	ContextPrefix string `yaml:"context_prefix"`
	None          string `yaml:"none"`
}

// Gate is the Strength Round gate Noul.
type Gate struct {
	Task    string `yaml:"task"`
	IfTrue  string `yaml:"if_true"`
	IfFalse string `yaml:"if_false"`
}

// Strength is the Strength Round grading Choice.
type Strength struct {
	Task    string                    `yaml:"task"`
	Options map[string]StrengthOption `yaml:"options"`
}

// StrengthOption is one grading option as a rubric object.
type StrengthOption struct {
	What     string   `yaml:"what"`
	NotFor   string   `yaml:"not_for"`
	Examples []string `yaml:"examples"`
}

// Default returns the embedded tuning file.
func Default() *Tuning {
	t, err := parse(embedded, "embedded")
	if err != nil {
		panic("tuning: embedded tuning.yaml is invalid: " + err.Error()) // caught by TestDefault
	}
	return t
}

// Load reads the tuning file named by src, or the embedded one when src
// names none.
func Load(src config.Tuning) (*Tuning, error) {
	if src.File == "" {
		return Default(), nil
	}
	raw, err := os.ReadFile(src.File)
	if err != nil {
		return nil, fmt.Errorf("tuning: %w", err)
	}
	return parse(raw, src.File)
}

func parse(raw []byte, source string) (*Tuning, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true) // a misspelled key is an error, not a silent default
	var t Tuning
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("tuning %s: %w", source, err)
	}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("tuning %s: %w", source, err)
	}
	sum := sha256.Sum256(raw)
	t.Source, t.Hash = source, hex.EncodeToString(sum[:6])
	return &t, nil
}

// Validate checks that every prompt is present, that option names the
// code depends on exist, and that weights are finite.
func (t *Tuning) Validate() error {
	var errs []error
	need := func(name, s string) {
		if strings.TrimSpace(s) == "" {
			errs = append(errs, fmt.Errorf("%s is empty", name))
		}
	}
	levels := func(name string, q ScoreQuestion) {
		need(name+".question", q.Question)
		if len(q.Levels) < 2 {
			errs = append(errs, fmt.Errorf("%s.levels: %d levels, want at least 2", name, len(q.Levels)))
		}
		for i, l := range q.Levels {
			need(fmt.Sprintf("%s.levels[%d]", name, i), l)
		}
	}
	noul := func(name string, q NoulQuestion) {
		need(name+".question", q.Question)
		need(name+".if_true", q.IfTrue)
		need(name+".if_false", q.IfFalse)
	}
	options := func(name string, m map[string]string, fixed ...string) {
		if len(m) == 0 {
			errs = append(errs, fmt.Errorf("%s: no options", name))
		}
		for k, v := range m {
			need(name+"."+k, v)
		}
		for _, k := range fixed {
			if _, ok := m[k]; !ok {
				errs = append(errs, fmt.Errorf("%s: missing fixed option %q", name, k))
			}
		}
	}
	weight := func(name string, w float64) {
		if math.IsNaN(w) || math.IsInf(w, 0) {
			errs = append(errs, fmt.Errorf("%s: %v is not a finite number", name, w))
		}
	}
	exactKeys := func(name string, m map[string]float64, keys ...string) {
		for _, k := range keys {
			if _, ok := m[k]; !ok {
				errs = append(errs, fmt.Errorf("%s: missing %q", name, k))
			}
		}
		for k, v := range m {
			if !slices.Contains(keys, k) {
				errs = append(errs, fmt.Errorf("%s: unknown key %q", name, k))
			}
			weight(name+"."+k, v)
			if v < 0 {
				errs = append(errs, fmt.Errorf("%s.%s: %v is negative", name, k, v))
			}
		}
	}

	if t.Version != Version {
		errs = append(errs, fmt.Errorf("version %d, want %d", t.Version, Version))
	}
	weight("match_score.intercept", t.MatchScore.Intercept)
	weight("match_score.role_match", t.MatchScore.RoleMatch)
	weight("match_score.experience_short", t.MatchScore.ExperienceShort)
	exactKeys("fit_score.credit", t.FitScore.Credit,
		string(domain.StrengthStrong), string(domain.StrengthPartial), string(domain.StrengthWeak))
	exactKeys("fit_score.tier_weight", t.FitScore.TierWeight,
		string(domain.TierRequired), string(domain.TierPreferred), string(domain.TierMentioned))
	for k, v := range t.FitScore.TierWeight {
		if v == 0 {
			errs = append(errs, fmt.Errorf("fit_score.tier_weight.%s: must be positive", k))
		}
	}

	levels("holistic.role_match", t.Holistic.RoleMatch)
	noul("holistic.experience_short", t.Holistic.ExperienceShort)
	noul("holistic.blocker", t.Holistic.Blocker)

	ex := t.Extractor
	need("extractor.section.question", ex.Section.Question)
	options("extractor.section.options", ex.Section.Options,
		string(domain.SectionRequired), string(domain.SectionPreferred), string(domain.SectionResponsibilities),
		string(domain.SectionCompany), string(domain.SectionBenefits), string(domain.SectionOther))
	if len(ex.Section.Options) != 6 {
		errs = append(errs, fmt.Errorf("extractor.section.options: %d options, want exactly the 6 Sections", len(ex.Section.Options)))
	}
	need("extractor.job_summary.system_prompt", ex.JobSummary.SystemPrompt)
	need("extractor.validation.question", ex.Validation.Question)
	options("extractor.validation.reject_options", ex.Validation.RejectOptions)
	need("extractor.refinement.filler.question", ex.Refinement.Filler.Question)
	need("extractor.refinement.filler.keep", ex.Refinement.Filler.Keep)
	options("extractor.refinement.filler.reasons", ex.Refinement.Filler.Reasons)
	need("extractor.refinement.duplicate.question", ex.Refinement.Duplicate.Question)
	options("extractor.refinement.duplicate.reasons", ex.Refinement.Duplicate.Reasons)
	need("extractor.refinement.alternative.question", ex.Refinement.Alternative.Question)
	options("extractor.refinement.alternative.reasons", ex.Refinement.Alternative.Reasons)
	levels("extractor.refinement.importance", ex.Refinement.Importance)

	ck := t.Checker
	need("checker.retrieval.question", ck.Retrieval.Question)
	need("checker.retrieval.none", ck.Retrieval.None)
	need("checker.gate.task", ck.Gate.Task)
	need("checker.gate.if_true", ck.Gate.IfTrue)
	need("checker.gate.if_false", ck.Gate.IfFalse)
	need("checker.strength.task", ck.Strength.Task)
	for _, k := range []domain.EvidenceStrength{domain.StrengthStrong, domain.StrengthPartial, domain.StrengthWeak, domain.StrengthNone} {
		if _, ok := ck.Strength.Options[string(k)]; !ok {
			errs = append(errs, fmt.Errorf("checker.strength.options: missing fixed option %q", k))
		}
	}
	for k, o := range ck.Strength.Options {
		need("checker.strength.options."+k+".what", o.What)
	}
	return errors.Join(errs...)
}

// MatchWeights returns the Match Score weights.
func (t *Tuning) MatchWeights() domain.MatchWeights {
	m := t.MatchScore
	return domain.MatchWeights{Intercept: m.Intercept, RoleMatch: m.RoleMatch, ExperienceShort: m.ExperienceShort}
}

// FitWeights returns the Fit Score credits and Tier weights.
func (t *Tuning) FitWeights() domain.FitWeights {
	w := domain.FitWeights{Credit: map[domain.EvidenceStrength]float64{}, TierWeight: map[domain.Tier]float64{}}
	for k, v := range t.FitScore.Credit {
		w.Credit[domain.EvidenceStrength(k)] = v
	}
	for k, v := range t.FitScore.TierWeight {
		w.TierWeight[domain.Tier(k)] = v
	}
	return w
}
