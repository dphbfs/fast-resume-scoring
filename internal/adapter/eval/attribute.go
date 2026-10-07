package eval

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

// Pipeline stages a missed Requirement is attributed to, latest stage first.
const (
	// StageFiller: validated, then dropped as Filler in the Refinement Round.
	StageFiller = "refinement_filler"
	// StageMerged: validated, then merged into another Requirement.
	StageMerged = "refinement_merged"
	// StageScoring: extracted, but the one-to-one matching paired the
	// prediction with a different label.
	StageScoring = "scoring"
	// StageNotSelected: a Chunk offered it, but Validation selected a
	// different Candidate.
	StageNotSelected = "validation_not_selected"
	// StageRejected: a Chunk offered it, but Validation rejected the Chunk.
	StageRejected = "validation_rejected"
	// StageSectionDropped: its sentence was dropped by Section or as a heading.
	StageSectionDropped = "section_dropped"
	// StageNoCandidate: its sentence was kept, but no Candidate names it.
	StageNoCandidate = "no_candidate"
	// StageNotInText: no sentence contains it (label paraphrase).
	StageNotInText = "not_in_text"
)

// MissCause explains why one expected Requirement was not found.
type MissCause struct {
	Expected string `json:"expected"`
	Stage    string `json:"stage"`
	Detail   string `json:"detail"`
}

// attributeMiss finds the latest pipeline stage that saw the Requirement and
// lost it, using the extraction trace.
func attributeMiss(req ExpectedRequirement, tr domain.Trace) MissCause {
	variants := append([]string{req.Value}, req.Aliases...)
	matches := func(text string) bool { return strictMatch(variants, text) || looseMatch(variants, text) }
	cause := func(stage, format string, args ...any) MissCause {
		return MissCause{Expected: req.Value, Stage: stage, Detail: fmt.Sprintf(format, args...)}
	}

	for _, x := range tr.Refinement {
		if !matches(x.Value) {
			continue
		}
		switch {
		case !x.Kept:
			return cause(StageFiller, "%q dropped as %s (keep p=%.2f)", x.Value, x.FillerKind, x.KeepP)
		case x.MergedInto != "":
			return cause(StageMerged, "%q merged into %q", x.Value, x.MergedInto)
		default:
			return cause(StageScoring, "extracted as %q but matched to another label", x.Value)
		}
	}

	// Prefer a Chunk whose Candidate matches strictly.
	var best *domain.TraceChunk
	for i := range tr.Chunks {
		tc := &tr.Chunks[i]
		for _, c := range tc.Candidates {
			if strictMatch(variants, c) {
				best = tc
				break
			}
			if best == nil && looseMatch(variants, c) {
				best = tc
			}
		}
		if best == tc && candidateStrict(tc, variants) {
			break
		}
	}
	if best != nil {
		if best.Selected != "" {
			return cause(StageNotSelected, "chunk %q (%s) selected %q (p=%.2f)", best.Text, best.Ref, best.Selected, best.SelectedP)
		}
		return cause(StageRejected, "chunk %q (%s) rejected as %s (requirement mass %.2f)",
			best.Text, best.Ref, best.RejectReason, best.RequirementMass)
	}

	for _, s := range tr.Sentences {
		if !containsAny(s.Text, variants) {
			continue
		}
		if s.Dropped {
			return cause(StageSectionDropped, "sentence %s labeled %s (heading %q): %q", s.Ref, s.Section, s.Heading, clip(s.Text))
		}
		return cause(StageNoCandidate, "sentence %s (%s) kept, but no Candidate names it: %q", s.Ref, s.Section, clip(s.Text))
	}
	return cause(StageNotInText, "no sentence contains %q or its aliases", req.Value)
}

func candidateStrict(tc *domain.TraceChunk, variants []string) bool {
	for _, c := range tc.Candidates {
		if strictMatch(variants, c) {
			return true
		}
	}
	return false
}

// containsAny reports whether text contains a variant as a whole word,
// case-insensitively.
func containsAny(text string, variants []string) bool {
	t := strings.ToLower(quotes.Replace(text))
	for _, v := range variants {
		v = strings.ToLower(quotes.Replace(v))
		if v == "" {
			continue
		}
		re := regexp.MustCompile(`(^|[^a-z0-9])` + regexp.QuoteMeta(v) + `($|[^a-z0-9+#])`)
		if re.MatchString(t) {
			return true
		}
	}
	return false
}

func clip(s string) string {
	if r := []rune(s); len(r) > 90 {
		return string(r[:90]) + "…"
	}
	return s
}

// attributeMisses explains every miss of a scored fixture.
func attributeMisses(exp Expected, s FixtureScore, tr *domain.Trace) []MissCause {
	if tr == nil {
		return nil
	}
	byValue := map[string]ExpectedRequirement{}
	for _, r := range exp.Requirements {
		byValue[r.Value] = r
	}
	causes := make([]MissCause, 0, len(s.Misses))
	for _, m := range s.Misses {
		causes = append(causes, attributeMiss(byValue[m], *tr))
	}
	return causes
}
