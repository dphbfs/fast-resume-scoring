package eval

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dphbfs/fast-resume-scoring/internal/app"
	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

// GoldenRequirements turns a golden fixture's labels into the Resume
// Checker's input: Requirements req_1..N in label order with their Tier,
// each referring to the first Job Description sentence that contains its
// value or an alias as whole words (none when no sentence does; the title
// line is skipped), and the Alternative Groups by ID.
func GoldenRequirements(f Fixture) domain.Result {
	sentences := app.SplitSentences(f.JD.Text)
	res := domain.Result{
		SchemaVersion:     domain.SchemaVersion,
		Requirements:      make([]domain.Requirement, len(f.Expected.Requirements)),
		AlternativeGroups: []domain.AlternativeGroup{},
		Context:           map[domain.Ref]domain.ContextSentence{},
	}
	ids := map[string]string{}
	for i, r := range f.Expected.Requirements {
		id := fmt.Sprintf("req_%d", i+1)
		ids[r.Value] = id
		req := domain.Requirement{ID: id, Value: r.Value, Refs: []domain.Ref{}, Tier: domain.Tier(r.Tier)}
		names := append([]string{r.Value}, r.Aliases...)
		for _, s := range sentences {
			if s.Text == f.JD.Title {
				continue
			}
			if slices.ContainsFunc(names, func(n string) bool { return containsWord(s.Text, n) }) {
				req.Refs = append(req.Refs, s.Ref)
				res.Context[s.Ref] = s
				break
			}
		}
		res.Requirements[i] = req
	}
	for i, g := range f.Expected.AlternativeGroups {
		var members []string
		for _, v := range g {
			members = append(members, ids[v])
		}
		res.AlternativeGroups = append(res.AlternativeGroups, domain.AlternativeGroup{ID: fmt.Sprintf("alt_%d", i+1), Members: members})
	}
	return res
}

// containsWord reports whether name occurs in text, case-insensitively,
// not inside a longer word ("Go" is not in "Google").
func containsWord(text, name string) bool {
	text, name = strings.ToLower(text), strings.ToLower(name)
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	for i := 0; ; {
		j := strings.Index(text[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if (start == 0 || !isWord(before)) && (end == len(text) || !isWord(after)) {
			return true
		}
		i = start + 1
	}
}

// CoverageTally counts Coverage outcomes for scored Requirements.
type CoverageTally struct {
	Total int `json:"total"`
	// Exact: predicted Coverage equals the label. Covered: both agree on
	// whether there is any evidence (none vs some).
	Exact   int `json:"exact"`
	Covered int `json:"covered"`
}

// PairNote is one Evidence Link the labels and the prediction disagree on.
type PairNote struct {
	Requirement string `json:"requirement"`
	Unit        string `json:"unit"`
	Text        string `json:"text"`
	Want        string `json:"want"`
	Got         string `json:"got"`
}

// CheckerScore is the evaluation of one (Job Description, Resume) pair.
type CheckerScore struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Resume string `json:"resume"`
	// Retrieval recall: labeled pairs whose Requirement the Retrieval Round
	// passed on for that unit; StrongPartial* counts only strong and
	// partial labels.
	LabeledPairs           int `json:"labeled_pairs"`
	Retrieved              int `json:"retrieved"`
	LabeledStrongPartial   int `json:"labeled_strong_partial"`
	RetrievedStrongPartial int `json:"retrieved_strong_partial"`
	// Evidence Links: predicted links, and those whose pair is labeled.
	PredictedLinks int `json:"predicted_links"`
	CorrectLinks   int `json:"correct_links"`
	// Strength agreement over correct links: exact, and within one level.
	StrengthExact int `json:"strength_exact"`
	StrengthNear  int `json:"strength_near"`
	// Coverage per Tier, and a label -> prediction confusion matrix.
	Coverage  map[string]CoverageTally  `json:"coverage"`
	Confusion map[string]map[string]int `json:"confusion"`
	// CoverageErrors are scored Requirements whose Coverage is wrong.
	CoverageErrors []PairNote `json:"coverage_errors"`
	// FitGot is the Fit Score of the predicted Coverage, FitWant the one of
	// the labeled Coverage, both over the scored (non-Skip) Requirements.
	FitGot  *int `json:"fit_got"`
	FitWant *int `json:"fit_want"`
	// GapsWant are the labeled Gaps (required items with Coverage none), by
	// value; a group lists its members joined by " / ".
	GapsWant []string `json:"gaps_want"`
	// MissedLinks are labeled pairs with no predicted link; FalseLinks are
	// predicted links with no label; StrengthDiffs disagree on strength.
	MissedLinks   []PairNote `json:"missed_links"`
	FalseLinks    []PairNote `json:"false_links"`
	StrengthDiffs []PairNote `json:"strength_diffs"`
	DurationMS    int64      `json:"duration_ms"`
	Error         string     `json:"error,omitempty"`
	// Baseline is the generative baseline's answer for the pair, when run.
	Baseline *BaselineScore `json:"baseline,omitempty"`
	// Result is stored so labels can be rescored offline; Trace is written
	// to a separate file next to the report (retrieval recall needs it).
	Result *domain.CoverageResult `json:"result,omitempty"`
	Trace  *domain.CheckTrace     `json:"-"`
}

// ScoreCheck compares a Resume Checker result with a pair's labels.
// Requirements in Skip are left out everywhere.
func ScoreCheck(f CheckerFixture, res domain.CoverageResult, trace domain.CheckTrace, fw domain.FitWeights) (CheckerScore, error) {
	s := CheckerScore{
		ID: f.ID, Title: f.Job.JD.Title, Resume: f.Expected.Resume,
		Coverage: map[string]CoverageTally{}, Confusion: map[string]map[string]int{},
	}
	text := map[string]string{}
	for _, u := range f.Units {
		text[u.ID] = u.Text
	}
	skip := map[string]bool{}
	for _, v := range f.Expected.Skip {
		skip[v] = true
	}

	// want[requirement][unit] = labeled strength.
	want := map[string]map[string]string{}
	for req, labels := range f.Expected.Links {
		want[req] = map[string]string{}
		for _, l := range labels {
			id, err := ResolveQuote(f.Units, l.Quote)
			if err != nil {
				return s, fmt.Errorf("%s: %q: %w", f.ID, req, err)
			}
			want[req][id] = l.Strength
		}
	}
	retrieved := map[string]map[string]bool{} // unit -> requirement values
	for _, u := range trace.Units {
		retrieved[u.ID] = map[string]bool{}
		for _, v := range u.Retrieved {
			retrieved[u.ID][v] = true
		}
	}

	var gotCovs, wantCovs []domain.RequirementCoverage
	for _, r := range res.Requirements {
		if skip[r.Value] {
			continue
		}
		gotCovs = append(gotCovs, r)
		wantCovs = append(wantCovs, domain.RequirementCoverage{
			ID: r.ID, Tier: r.Tier,
			Coverage: domain.EvidenceStrength(bestStrength(want[r.Value])),
		})
		got := map[string]string{}
		for _, l := range r.Evidence {
			got[l.Unit] = string(l.Strength)
		}
		for _, unit := range slices.Sorted(maps.Keys(want[r.Value])) {
			w := want[r.Value][unit]
			s.LabeledPairs++
			sp := w != StrengthWeak
			if sp {
				s.LabeledStrongPartial++
			}
			if retrieved[unit][r.Value] {
				s.Retrieved++
				if sp {
					s.RetrievedStrongPartial++
				}
			}
			g, ok := got[unit]
			if !ok {
				s.MissedLinks = append(s.MissedLinks, PairNote{r.Value, unit, text[unit], w, "none"})
				continue
			}
			s.CorrectLinks++
			switch strengthRank[w] - strengthRank[g] {
			case 0:
				s.StrengthExact++
				s.StrengthNear++
			case 1, -1:
				s.StrengthNear++
				s.StrengthDiffs = append(s.StrengthDiffs, PairNote{r.Value, unit, text[unit], w, g})
			default:
				s.StrengthDiffs = append(s.StrengthDiffs, PairNote{r.Value, unit, text[unit], w, g})
			}
		}
		for _, unit := range slices.Sorted(maps.Keys(got)) {
			s.PredictedLinks++
			if _, ok := want[r.Value][unit]; !ok {
				s.FalseLinks = append(s.FalseLinks, PairNote{r.Value, unit, text[unit], "none", got[unit]})
			}
		}

		wantCov := bestStrength(want[r.Value])
		gotCov := string(r.Coverage)
		tier := string(r.Tier)
		t := s.Coverage[tier]
		t.Total++
		if wantCov == gotCov {
			t.Exact++
		} else {
			s.CoverageErrors = append(s.CoverageErrors, PairNote{Requirement: r.Value, Want: wantCov, Got: gotCov})
		}
		if (wantCov == "none") == (gotCov == "none") {
			t.Covered++
		}
		s.Coverage[tier] = t
		if s.Confusion[wantCov] == nil {
			s.Confusion[wantCov] = map[string]int{}
		}
		s.Confusion[wantCov][gotCov]++
	}
	s.FitGot = domain.ScoreFit(gotCovs, res.AlternativeGroups, fw).Score
	wantFit := domain.ScoreFit(wantCovs, res.AlternativeGroups, fw)
	s.FitWant = wantFit.Score
	s.GapsWant = gapValues(wantFit.Gaps, res)
	return s, nil
}

// gapValues turns Gap IDs (Requirement or Alternative Group) into values.
func gapValues(ids []string, res domain.CoverageResult) []string {
	value := map[string]string{}
	for _, r := range res.Requirements {
		value[r.ID] = r.Value
	}
	for _, g := range res.AlternativeGroups {
		var vs []string
		for _, m := range g.Members {
			vs = append(vs, value[m])
		}
		value[g.ID] = strings.Join(vs, " / ")
	}
	out := []string{}
	for _, id := range ids {
		out = append(out, value[id])
	}
	return out
}

// bestStrength is the Coverage the labels imply.
func bestStrength(units map[string]string) string {
	best := "none"
	for _, st := range units {
		if strengthRank[st] > strengthRank[best] {
			best = st
		}
	}
	return best
}
