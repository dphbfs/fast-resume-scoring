package app

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// Checker runs the Resume Checker: it parses a Resume into Evidence Units,
// proposes Requirements per unit (Retrieval Round), judges each proposed
// pair (Strength Round), and reports Coverage per Requirement.
type Checker struct {
	classifier port.AIClassifierClient
	metrics    port.Metrics
	log        *slog.Logger
	cfg        config.Checker
}

var _ port.ResumeChecker = (*Checker)(nil)

// NewChecker builds a Checker.
func NewChecker(classifier port.AIClassifierClient, m port.Metrics, log *slog.Logger, cfg config.Checker) *Checker {
	return &Checker{classifier: classifier, metrics: m, log: log.With("component", "checker"), cfg: cfg}
}

// noneOption is the Retrieval Round's sink option.
const noneOption = "none"

// checkRequirement is one Requirement as offered to Jev.
type checkRequirement struct {
	domain.Requirement
	// option is the Retrieval option key: the value, made unique.
	option string
	// context is its shortest Context Sentence, "" when it has none.
	context string
}

// unitMatch is the Retrieval and Strength answers for one Evidence Unit.
type unitMatch struct {
	links map[int]domain.EvidenceLink // by Requirement index
	trace domain.TraceUnit
}

// Check links the Resume's Evidence Units to reqs and reports Coverage.
func (c *Checker) Check(ctx context.Context, reqs domain.Result, resume domain.Resume) (domain.CoverageResult, domain.CheckTrace, error) {
	var trace domain.CheckTrace
	var units []domain.EvidenceUnit
	if err := c.stage(ctx, "parse_resume", func(context.Context) error {
		units = ParseResume(resume.Text)
		if len(units) == 0 {
			return fmt.Errorf("resume has no evidence units")
		}
		c.metrics.Add("checker.units", int64(len(units)))
		return nil
	}); err != nil {
		return domain.CoverageResult{}, trace, err
	}
	if len(reqs.Requirements) == 0 {
		return domain.CoverageResult{}, trace, fmt.Errorf("no requirements to check")
	}
	creqs := checkRequirements(reqs)

	matches := make([]unitMatch, len(units))
	var model string
	if err := c.stage(ctx, "match_evidence", func(ctx context.Context) error {
		var err error
		model, err = c.matchAll(ctx, units, creqs, matches)
		return err
	}); err != nil {
		return domain.CoverageResult{}, trace, err
	}
	for _, m := range matches {
		trace.Units = append(trace.Units, m.trace)
	}

	var res domain.CoverageResult
	_ = c.stage(ctx, "build_coverage", func(context.Context) error {
		res = c.buildCoverage(reqs, creqs, units, matches, model)
		return nil
	})
	return res, trace, nil
}

// checkRequirements pairs each Requirement with a unique option key and its
// shortest Context Sentence.
func checkRequirements(reqs domain.Result) []checkRequirement {
	out := make([]checkRequirement, len(reqs.Requirements))
	used := map[string]bool{strings.ToLower(noneOption): true}
	for i, r := range reqs.Requirements {
		opt := r.Value
		if used[strings.ToLower(opt)] {
			opt = fmt.Sprintf("%s (%s)", r.Value, r.ID)
		}
		used[strings.ToLower(opt)] = true
		ctx := ""
		for _, ref := range r.Refs {
			if s, ok := reqs.Context[ref]; ok && (ctx == "" || len(s.Text) < len(ctx)) {
				ctx = s.Text
			}
		}
		out[i] = checkRequirement{Requirement: r, option: opt, context: ctx}
	}
	return out
}

// matchAll runs both rounds for every Evidence Unit concurrently (the Jev
// client bounds the requests in flight). Each unit's Strength request
// starts as soon as its Retrieval answer is in. It returns the Jev model.
func (c *Checker) matchAll(ctx context.Context, units []domain.EvidenceUnit, creqs []checkRequirement, out []unitMatch) (string, error) {
	models := make([]string, len(units))
	g, gctx := errgroup.WithContext(ctx)
	for i, u := range units {
		g.Go(func() error {
			start := time.Now()
			retrieved, rounds, model, err := c.retrieve(gctx, u, creqs)
			if err != nil {
				return fmt.Errorf("retrieval %s: %w", u.ID, err)
			}
			c.metrics.ObserveDuration("checker.retrieval", time.Since(start))
			models[i] = model
			m := unitMatch{links: map[int]domain.EvidenceLink{}, trace: domain.TraceUnit{
				ID: u.ID, Text: u.Text, ResumeSection: u.ResumeSection, Rounds: rounds, Retrieved: []string{}, Pairs: []domain.TracePair{},
			}}
			for _, ri := range retrieved {
				m.trace.Retrieved = append(m.trace.Retrieved, creqs[ri].Value)
			}
			if len(retrieved) > 0 {
				start = time.Now()
				if err := c.judgeStrength(gctx, u, creqs, retrieved, &m); err != nil {
					return fmt.Errorf("strength %s: %w", u.ID, err)
				}
				c.metrics.ObserveDuration("checker.strength", time.Since(start))
			}
			out[i] = m
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return "", err
	}
	for _, m := range models {
		if m != "" {
			return m, nil
		}
	}
	return "", nil
}

// evidenceState is the Jev state for one Evidence Unit.
type evidenceState struct {
	ResumeSection domain.ResumeSection `json:"resume_section"`
	Role          string               `json:"role,omitempty"`
	Company       string               `json:"company,omitempty"`
	Statement     string               `json:"statement"`
}

func stateOf(u domain.EvidenceUnit) evidenceState {
	return evidenceState{ResumeSection: u.ResumeSection, Role: u.Role, Company: u.Company, Statement: u.Text}
}

// strengthCriteriaV1 define the Evidence Strengths (first version).
var strengthCriteriaV1 = map[string]any{
	string(domain.StrengthStrong): "The statement shows hands-on work with the requirement itself or a direct instance of it " +
		"(requirement Kubernetes, statement: ran services on EKS).",
	string(domain.StrengthPartial): "The statement shows work with a close neighbor or a part of the requirement, " +
		"or names it with only light involvement (requirement Kubernetes, statement: containerized services with Docker).",
	string(domain.StrengthWeak): "The statement only names, lists, or implies the requirement, with no demonstrated work " +
		"(a skills list or a self-description).",
	string(domain.StrengthNone): "The statement does not show this requirement.",
}

// strengthCriteriaV2 sharpens strong vs partial (is the requirement itself
// the focus of the work?) and covers degree and certificate entries, which
// v1's weak ("only names") absorbed. Examples avoid the eval fixtures.
var strengthCriteriaV2 = map[string]any{
	string(domain.StrengthStrong): "The requirement itself, or a specific instance of it, is what the described work was done with " +
		"or on (requirement Spark: wrote PySpark jobs on EMR). An education or certification entry is strong evidence " +
		"for the degree or certificate it names.",
	string(domain.StrengthPartial): "The work involves a part, a prerequisite, or the broader practice of the requirement, or uses " +
		"the requirement only lightly, but the requirement itself is not the focus (requirement Spark: built nightly " +
		"batch data jobs; requirement code review: paired with teammates on fixes).",
	string(domain.StrengthWeak): "The statement only names, lists, or claims the requirement without describing work with it " +
		"(a skills list, a self-description, an interest).",
	string(domain.StrengthNone): "The statement has nothing to do with this requirement.",
}

// strengthNegatives name why a pair that looks related is not evidence, so
// probability for these cases does not fall on partial. Examples avoid the
// eval fixtures.
var strengthNegatives = map[string]any{
	"alternative_tool": "The statement uses a different tool, product, or framework of the same kind, one that could " +
		"replace the requirement rather than include it (requirement Spark: built Flink jobs; requirement Git: Mercurial).",
	"shared_words_only": "The statement shares a word or a topic with the requirement but describes different work " +
		"(requirement event sourcing: organized company events; requirement distributed tracing: traced a bug's cause).",
	"different_skill": "The statement shows real but different skills that do not exercise the requirement " +
		"(requirement Spark: tuned SQL queries; requirement mentoring: wrote API docs).",
	"context_only": "Only the role, team, or product around the statement suggests the requirement; the statement itself " +
		"does not show it (requirement Swift: mentored interns on an iOS team).",
}

// neededCapability is the v4 negative: work that would need the
// Requirement, without showing it was used.
var neededCapability = map[string]any{
	"needed_capability": "The described work would usually need the requirement, but the statement does not show it was " +
		"used (requirement Linux: deployed a web app; requirement OAuth: integrated a payment provider).",
}

// strengthCriteriaV5 is v3 as structured rubric objects: each option says
// what it covers, what it does not, and gives examples, which the TypeSafe
// docs recommend for sharpening boundaries between options. Examples avoid
// the eval fixtures.
var strengthCriteriaV5 = map[string]any{
	string(domain.StrengthStrong): map[string]any{
		"what": "The requirement itself, or a specific instance of it, is what the described work was done with or on. " +
			"An education or certification entry is strong evidence for the degree or certificate it names.",
		"not_for": "Work on a part, prerequisite, or the broader practice of the requirement; work where the requirement " +
			"is a minor detail; work with a competing tool.",
		"examples": []string{"requirement Spark: wrote PySpark jobs on EMR", "requirement code review: reviewed every pull request for a team of six"},
	},
	string(domain.StrengthPartial): map[string]any{
		"what": "The work involves a part, a prerequisite, or the broader practice of the requirement, or uses the " +
			"requirement only as a minor detail.",
		"not_for": "Work centered on the requirement itself (strong); a competing tool of the same kind; work that only " +
			"shares words with it.",
		"examples": []string{"requirement Spark: built nightly batch data jobs", "requirement code review: paired with teammates on fixes"},
	},
	string(domain.StrengthWeak): map[string]any{
		"what":     "The statement only names, lists, or claims the requirement without describing work with it.",
		"not_for":  "Any statement that describes work done with the requirement.",
		"examples": []string{"Skills: Spark, Kafka, Airflow", "passionate about clean code reviews"},
	},
	string(domain.StrengthNone): map[string]any{
		"what": "The statement has nothing to do with this requirement.",
	},
	"alternative_tool": map[string]any{
		"what":     "The statement uses a different tool, product, or framework of the same kind, one that could replace the requirement.",
		"not_for":  "A tool that is part of or built on the requirement (that is partial or strong).",
		"examples": []string{"requirement Spark: built Flink jobs", "requirement Git: used Mercurial"},
	},
	"shared_words_only": map[string]any{
		"what":     "The statement shares a word or a topic with the requirement but describes different work.",
		"examples": []string{"requirement event sourcing: organized company events", "requirement distributed tracing: traced a bug's cause"},
	},
	"different_skill": map[string]any{
		"what":     "The statement shows real but different skills that do not exercise the requirement.",
		"examples": []string{"requirement Spark: tuned SQL queries", "requirement mentoring: wrote API docs"},
	},
	"context_only": map[string]any{
		"what":     "Only the role, team, or product around the statement suggests the requirement; the statement itself does not show it.",
		"examples": []string{"requirement Swift: mentored interns on an iOS team"},
	},
}

// strengthCriteria returns the Strength options for cfg.StrengthCriteria:
// v1, v2 (sharpened), v3 (v2 plus the negative options, the default), or
// v4 (v3 plus needed_capability), or v5 (v3 as structured rubric objects).
func (c *Checker) strengthCriteria() map[string]any {
	switch c.cfg.StrengthCriteria {
	case "v1":
		return strengthCriteriaV1
	case "v2":
		return strengthCriteriaV2
	case "v5":
		return strengthCriteriaV5
	}
	out := maps.Clone(strengthCriteriaV2)
	maps.Copy(out, strengthNegatives)
	if c.cfg.StrengthCriteria == "v4" {
		maps.Copy(out, neededCapability)
	}
	return out
}

// strengthQuestion asks how strongly the unit demonstrates one Requirement.
// The Requirement is embedded because question IDs are not sent to Jev.
func strengthQuestion(r checkRequirement, criteria map[string]any) port.Question {
	inst := map[string]any{
		"task":        "How strongly does the resume `statement` demonstrate this job requirement?",
		"requirement": r.Value,
	}
	if r.context != "" {
		inst["job_posting_context"] = r.context
	}
	return port.Question{Type: port.Choice, Instructions: inst, Criteria: criteria}
}

// gateTrue is the gate Noul's yes criterion by wording version. v1 covers
// only the requirement itself, which excluded partial evidence (a part,
// prerequisite, or the broader practice) and so contradicted the Strength
// criteria; v2 includes it.
var gateTrue = map[string]string{
	"v1": "The statement describes work done with the requirement itself or a specific instance of it, or " +
		"names it as the candidate's own skill, degree, or certificate.",
	"v2": "The statement describes work done with the requirement itself, a specific instance of it, a part or " +
		"prerequisite of it, or its broader practice; or it names the requirement as the candidate's own skill, " +
		"degree, or certificate.",
}

// gateQuestion asks, absolutely, whether the unit is evidence for one
// Requirement. The TypeSafe skill-suggestion pattern: a Choice grades, an
// independent Noul decides whether to act at all.
func gateQuestion(r checkRequirement, wording string) port.Question {
	inst := map[string]any{
		"task":        "Is the resume `statement` evidence that the candidate has this job requirement?",
		"requirement": r.Value,
	}
	if r.context != "" {
		inst["job_posting_context"] = r.context
	}
	return port.Question{
		Type:         port.Noul,
		Instructions: inst,
		Criteria: map[string]any{
			"true": gateTrue[wording],
			"false": "The statement uses a competing tool, only shares words with the requirement, shows different " +
				"skills, or only its surrounding role suggests the requirement.",
		},
	}
}

// strengthLevels are the Score levels for grading mode "score", lowest
// first; level i maps to strengthByLevel[i]. Whether the pair is evidence
// at all is the gate's job, so there is no "none" level.
var strengthLevels = []any{
	"The statement only names, lists, or claims the requirement, without describing work done with it " +
		"(Skills: Spark, Kafka).",
	"The described work involves a part, a prerequisite, or the broader practice of the requirement, or uses the " +
		"requirement only as a minor detail (requirement Spark: built nightly batch data jobs).",
	"The requirement itself, or a specific instance of it, is what the described work was done with or on " +
		"(requirement Spark: wrote PySpark jobs on EMR). An education or certification entry for the degree or " +
		"certificate the requirement names.",
}

var strengthByLevel = []domain.EvidenceStrength{domain.StrengthWeak, domain.StrengthPartial, domain.StrengthStrong}

// scoreQuestion grades one Requirement on strengthLevels.
func scoreQuestion(r checkRequirement) port.Question {
	inst := map[string]any{
		"task":        "How central is this job requirement to the work described in the resume `statement`?",
		"requirement": r.Value,
	}
	if r.context != "" {
		inst["job_posting_context"] = r.context
	}
	return port.Question{Type: port.Score, Instructions: inst, Criteria: strengthLevels}
}

// judgeStrength asks one Strength question per retrieved Requirement, in one
// request, and records the Evidence Links that pass: the gate Noul at or
// above GateThreshold when the gate is on, else MinEvidenceMass.
func (c *Checker) judgeStrength(ctx context.Context, u domain.EvidenceUnit, creqs []checkRequirement, retrieved []int, m *unitMatch) error {
	criteria := c.strengthCriteria()
	questions := make(map[string]port.Question, len(retrieved))
	for k, ri := range retrieved {
		if c.cfg.StrengthMode == "score" {
			questions[fmt.Sprintf("req_%d", k)] = scoreQuestion(creqs[ri])
		} else {
			questions[fmt.Sprintf("req_%d", k)] = strengthQuestion(creqs[ri], criteria)
		}
		if c.cfg.GateThreshold > 0 {
			questions[fmt.Sprintf("gate_%d", k)] = gateQuestion(creqs[ri], c.gateWording())
		}
	}
	resp, err := c.classifier.Classify(ctx, port.ClassifyRequest{State: stateOf(u), Questions: questions})
	if err != nil {
		return err
	}
	c.metrics.Add("checker.strength.requests", 1)
	capped := u.ResumeSection == domain.ResumeSkills || u.ResumeSection == domain.ResumeSummary
	for k, ri := range retrieved {
		a := resp.Answers[fmt.Sprintf("req_%d", k)]
		var strength domain.EvidenceStrength
		var mass float64
		if c.cfg.StrengthMode == "score" {
			if a.Score == nil {
				return fmt.Errorf("requirement %q: no score answer", creqs[ri].Value)
			}
			strength, mass = levelStrength(a.Probabilities), 1
		} else {
			if _, ok := criteria[a.Choice]; !ok {
				return fmt.Errorf("requirement %q: answer %q is not a strength", creqs[ri].Value, a.Choice)
			}
			strength, mass = decideStrength(a.Probabilities)
		}
		tp := domain.TracePair{Requirement: creqs[ri].Value, Probabilities: a.Probabilities, EvidenceMass: mass}
		pass := mass >= c.minEvidenceMass()
		if c.cfg.GateThreshold > 0 {
			g := resp.Answers[fmt.Sprintf("gate_%d", k)]
			if g.Noul == nil {
				return fmt.Errorf("requirement %q: no gate answer", creqs[ri].Value)
			}
			tp.Gate = g.Noul
			pass = *g.Noul >= c.cfg.GateThreshold
		}
		if !pass {
			tp.RejectReason = topRejection(a.Probabilities)
			c.metrics.Add("checker.strength.rejected."+tp.RejectReason, 1)
			c.log.DebugContext(ctx, "evidence rejected", "unit", u.ID, "requirement", creqs[ri].Value,
				"evidence_mass", mass, "reason", tp.RejectReason)
			m.trace.Pairs = append(m.trace.Pairs, tp)
			continue
		}
		if capped && strength.Rank() > domain.StrengthWeak.Rank() {
			strength, tp.Capped = domain.StrengthWeak, true
			c.metrics.Add("checker.strength.capped", 1)
		}
		tp.Linked, tp.Strength = true, strength
		m.links[ri] = domain.EvidenceLink{Unit: u.ID, Strength: strength, P: mass}
		m.trace.Pairs = append(m.trace.Pairs, tp)
		c.metrics.Add("checker.strength.linked."+string(strength), 1)
		c.log.DebugContext(ctx, "evidence linked", "unit", u.ID, "requirement", creqs[ri].Value,
			"strength", strength, "evidence_mass", mass, "capped", tp.Capped)
	}
	return nil
}

// levelStrength maps a Score answer to the most probable level's strength.
// Score probabilities are keyed by level index ("0", "1", "2").
func levelStrength(probs map[string]float64) domain.EvidenceStrength {
	best, bestP := 0, -1.0
	for i := range strengthByLevel {
		if p := probs[strconv.Itoa(i)]; p > bestP {
			best, bestP = i, p
		}
	}
	return strengthByLevel[best]
}

// topRejection returns the most probable non-evidence option (none or a
// negative).
func topRejection(probs map[string]float64) string {
	best, bestP := string(domain.StrengthNone), -1.0
	for _, o := range slices.Sorted(maps.Keys(probs)) {
		if domain.EvidenceStrength(o).Rank() > 0 {
			continue
		}
		if probs[o] > bestP {
			best, bestP = o, probs[o]
		}
	}
	return best
}

// decideStrength returns the most probable of strong/partial/weak and their
// summed probability (the evidence mass).
func decideStrength(probs map[string]float64) (domain.EvidenceStrength, float64) {
	best, bestP, mass := domain.StrengthWeak, -1.0, 0.0
	for _, s := range []domain.EvidenceStrength{domain.StrengthStrong, domain.StrengthPartial, domain.StrengthWeak} {
		p := probs[string(s)]
		mass += p
		if p > bestP {
			best, bestP = s, p
		}
	}
	return best, mass
}

// buildCoverage assembles the coverage schema v1 result: Requirements in
// input order, each with its links (strongest first) and Coverage, groups
// with their best member's Coverage, and the units any link refers to.
func (c *Checker) buildCoverage(reqs domain.Result, creqs []checkRequirement, units []domain.EvidenceUnit, matches []unitMatch, model string) domain.CoverageResult {
	unitByID := make(map[string]domain.EvidenceUnit, len(units))
	for _, u := range units {
		unitByID[u.ID] = u
	}
	res := domain.CoverageResult{
		SchemaVersion:     domain.CoverageSchemaVersion,
		Model:             model,
		Requirements:      make([]domain.RequirementCoverage, len(creqs)),
		AlternativeGroups: []domain.GroupCoverage{},
		EvidenceUnits:     map[string]domain.EvidenceUnit{},
	}
	coverage := map[string]domain.EvidenceStrength{}
	for i, r := range creqs {
		rc := domain.RequirementCoverage{ID: r.ID, Value: r.Value, Tier: r.Tier, Coverage: domain.StrengthNone, Evidence: []domain.EvidenceLink{}}
		for _, m := range matches {
			if l, ok := m.links[i]; ok {
				rc.Evidence = append(rc.Evidence, l)
				res.EvidenceUnits[l.Unit] = unitByID[l.Unit]
			}
		}
		slices.SortStableFunc(rc.Evidence, func(a, b domain.EvidenceLink) int {
			if d := b.Strength.Rank() - a.Strength.Rank(); d != 0 {
				return d
			}
			switch {
			case a.P > b.P:
				return -1
			case a.P < b.P:
				return 1
			}
			return 0
		})
		if len(rc.Evidence) > 0 {
			rc.Coverage = rc.Evidence[0].Strength
		}
		coverage[r.ID] = rc.Coverage
		res.Requirements[i] = rc
		c.metrics.Add("checker.coverage."+string(rc.Coverage), 1)
	}
	for _, g := range reqs.AlternativeGroups {
		best := domain.StrengthNone
		for _, id := range g.Members {
			if s := coverage[id]; s.Rank() > best.Rank() {
				best = s
			}
		}
		res.AlternativeGroups = append(res.AlternativeGroups, domain.GroupCoverage{ID: g.ID, Members: g.Members, Coverage: best})
	}
	res.Fit = domain.ScoreFit(res.Requirements, res.AlternativeGroups)
	c.metrics.Add("checker.gaps", int64(len(res.Fit.Gaps)))
	if res.Fit.Score != nil {
		c.log.Info("fit score", "score", *res.Fit.Score, "by_tier", res.Fit.ByTier, "gaps", len(res.Fit.Gaps))
	}
	return res
}

func (c *Checker) retrievalMode() string {
	if c.cfg.RetrievalMode != "" {
		return c.cfg.RetrievalMode
	}
	return "single"
}

func (c *Checker) gateWording() string {
	if c.cfg.GateWording != "" {
		return c.cfg.GateWording
	}
	return "v1"
}

func (c *Checker) noulThreshold() float64 {
	if c.cfg.NoulThreshold > 0 {
		return c.cfg.NoulThreshold
	}
	return 0.5
}

func (c *Checker) retrievalK() int {
	if c.cfg.RetrievalK > 0 {
		return c.cfg.RetrievalK
	}
	return 5
}

func (c *Checker) retrievalFloor() float64 {
	if c.cfg.RetrievalFloor > 0 {
		return c.cfg.RetrievalFloor
	}
	return 0.02
}

func (c *Checker) minEvidenceMass() float64 {
	if c.cfg.MinEvidenceMass > 0 {
		return c.cfg.MinEvidenceMass
	}
	return 0.5
}

// stage runs fn with timing, metrics and structured logs.
func (c *Checker) stage(ctx context.Context, name string, fn func(context.Context) error) error {
	start := time.Now()
	err := fn(ctx)
	elapsed := time.Since(start)
	c.metrics.ObserveDuration("stage."+name, elapsed)
	if err != nil {
		c.log.ErrorContext(ctx, "stage failed", "stage", name, "duration_ms", elapsed.Milliseconds(), "error", err)
		return fmt.Errorf("%s: %w", name, err)
	}
	c.log.InfoContext(ctx, "stage done", "stage", name, "duration_ms", elapsed.Milliseconds())
	return nil
}
