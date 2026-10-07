package eval

import (
	"regexp"
	"slices"
	"strings"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

// Tier ranks, highest first; Importance should order Requirements this way.
var tierRank = map[string]int{"required": 3, "preferred": 2, "mentioned": 1}

// Match pairs one expected Requirement with the predicted one it matched.
type Match struct {
	Expected  string `json:"expected"`
	Predicted string `json:"predicted"`
	Strict    bool   `json:"strict"`
}

// TierScore is recall within one tier, and how often matched predictions
// carry that tier.
type TierScore struct {
	Expected int `json:"expected"`
	Matched  int `json:"matched"`
	// TierChecked counts matched predictions with a known Tier (stored or
	// derived from Context); TierCorrect those whose Tier equals the
	// label's.
	TierChecked int `json:"tier_checked,omitempty"`
	TierCorrect int `json:"tier_correct,omitempty"`
}

// FixtureScore is the evaluation of one fixture.
type FixtureScore struct {
	ID            string               `json:"id"`
	Title         string               `json:"title"`
	Expected      int                  `json:"expected"`
	Predicted     int                  `json:"predicted"`
	StrictMatched int                  `json:"strict_matched"`
	LooseMatched  int                  `json:"loose_matched"` // strict + loose
	Tiers         map[string]TierScore `json:"tiers"`
	// TierOrder is the share of matched cross-tier pairs whose Importance
	// orders them like their tiers (ties count half). Nil when every
	// Importance is equal (no Refinement Round yet).
	TierOrder *float64 `json:"tier_order,omitempty"`
	// GroupF1 is pair-level F1 of Alternative Groups over matched
	// Requirements. Nil when the result has no groups.
	GroupF1        *float64 `json:"group_f1,omitempty"`
	Matches        []Match  `json:"matches"`
	Misses         []string `json:"misses"`
	Extras         []string `json:"extras"`
	FillerHits     []string `json:"filler_hits"`
	AcceptableHits []string `json:"acceptable_hits"`
	// DuplicateHits are predictions matching a Requirement that another
	// prediction already matched: a deduplication miss, not a wrong
	// extraction.
	DuplicateHits []string `json:"duplicate_hits"`
	DurationMS    int64    `json:"duration_ms"`
	Error         string   `json:"error,omitempty"`
	// Result is the extractor output, stored so labels can be rescored
	// offline (eval -rescore).
	Result *domain.Result `json:"result,omitempty"`
	// MissCauses attributes each miss to a pipeline stage (needs a trace).
	MissCauses []MissCause `json:"miss_causes,omitempty"`
	// Trace is written to a separate file next to the report.
	Trace *domain.Trace `json:"-"`
}

var (
	dropChars = regexp.MustCompile(`[^a-z0-9+#./'\s-]`)
	spaces    = regexp.MustCompile(`\s+`)
	quotes    = strings.NewReplacer("’", "'", "‘", "'", "“", "", "”", "", "—", " ", "–", " ")
)

// normalize lowercases, straightens quotes, and drops punctuation other than
// the characters technical names use (+ # . / ' -).
func normalize(s string) string {
	s = dropChars.ReplaceAllString(strings.ToLower(quotes.Replace(s)), " ")
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "with": true,
	"by": true, "from": true, "as": true, "is": true, "are": true, "be": true,
	"experience": true, "strong": true, "proficiency": true, "knowledge": true,
}

// content returns the normalized tokens that carry meaning. Slash- and
// hyphen-joined words are split ("terraform/terragrunt", "GitOps-style"),
// and each token is stemmed.
func content(s string) []string {
	var out []string
	for f := range strings.FieldsSeq(normalize(s)) {
		for _, t := range strings.FieldsFunc(f, func(r rune) bool { return r == '/' || r == '-' }) {
			t = strings.Trim(t, ".'")
			if t != "" && !stopwords[t] {
				out = append(out, stem(t))
			}
		}
	}
	return out
}

// suffixes are stripped by stem, longest first.
var suffixes = []string{"izations", "ization", "ments", "ment", "ships", "ship", "ings", "ing", "ions", "ion", "es", "ed", "s"}

// stem strips one common English suffix, keeping at least 4 letters, so
// "alerts", "alerting" -> "alert" and "mentorship", "mentoring" -> "mentor".
func stem(t string) string {
	for _, suf := range suffixes {
		if strings.HasSuffix(t, suf) && len(t)-len(suf) >= 4 {
			return t[:len(t)-len(suf)]
		}
	}
	return t
}

// tokEq matches equal stems, or stems of 5+ letters where one is a prefix
// of the other ("modell"/"model", "productioniz"/"productionize").
func tokEq(a, b string) bool {
	if a == b {
		return true
	}
	if min(len(a), len(b)) < 5 {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func containsTok(list []string, t string) bool {
	return slices.ContainsFunc(list, func(x string) bool { return tokEq(x, t) })
}

func subset(a, b []string) bool {
	for _, x := range a {
		if !containsTok(b, x) {
			return false
		}
	}
	return true
}

func jaccard(a, b []string) float64 {
	inter := 0
	for _, x := range a {
		if containsTok(b, x) {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// strictMatch: the prediction equals the value or an alias after
// normalization.
func strictMatch(variants []string, predicted string) bool {
	p := normalize(predicted)
	for _, v := range variants {
		if normalize(v) == p {
			return true
		}
	}
	return false
}

// looseMatch accepts a prediction that pads a variant with up to 3 extra
// words ("experience with Kafka"), shortens a multi-word variant by one word
// ("streaming data"), or overlaps it with Jaccard >= 0.6.
func looseMatch(variants []string, predicted string) bool {
	tp := content(predicted)
	if len(tp) == 0 {
		return false
	}
	for _, v := range variants {
		tv := content(v)
		switch {
		case len(tv) == 0:
		case subset(tv, tp) && len(tp)-len(tv) <= 3:
			return true
		case subset(tp, tv) && len(tp) >= 2 && len(tv)-len(tp) <= 1:
			return true
		case jaccard(tv, tp) >= 0.6:
			return true
		}
	}
	return false
}

// Score compares one Result with its expected labels. Expected and predicted
// Requirements are matched one-to-one, strict matches first.
func Score(exp Expected, res domain.Result) FixtureScore {
	s := FixtureScore{
		Expected:       len(exp.Requirements),
		Predicted:      len(res.Requirements),
		Tiers:          map[string]TierScore{},
		Matches:        []Match{},
		Misses:         []string{},
		Extras:         []string{},
		FillerHits:     []string{},
		AcceptableHits: []string{},
		DuplicateHits:  []string{},
	}
	variants := make([][]string, len(exp.Requirements))
	for i, e := range exp.Requirements {
		variants[i] = append([]string{e.Value}, e.Aliases...)
	}

	matchedPred := make([]int, len(exp.Requirements)) // expected -> predicted index, -1 if none
	usedPred := make([]bool, len(res.Requirements))
	for i := range matchedPred {
		matchedPred[i] = -1
	}
	for pass, match := range []func([]string, string) bool{strictMatch, looseMatch} {
		for i := range exp.Requirements {
			if matchedPred[i] >= 0 {
				continue
			}
			best, bestJ := -1, -1.0
			for j, p := range res.Requirements {
				if usedPred[j] || !match(variants[i], p.Value) {
					continue
				}
				if jv := jaccard(content(exp.Requirements[i].Value), content(p.Value)); jv > bestJ {
					best, bestJ = j, jv
				}
			}
			if best >= 0 {
				matchedPred[i], usedPred[best] = best, true
				s.Matches = append(s.Matches, Match{
					Expected: exp.Requirements[i].Value, Predicted: res.Requirements[best].Value, Strict: pass == 0,
				})
				if pass == 0 {
					s.StrictMatched++
				}
				s.LooseMatched++
			}
		}
	}

	for i, e := range exp.Requirements {
		ts := s.Tiers[e.Tier]
		ts.Expected++
		if j := matchedPred[i]; j >= 0 {
			ts.Matched++
			if tier := tierOf(res, j); tier != "" {
				ts.TierChecked++
				if string(tier) == e.Tier {
					ts.TierCorrect++
				}
			}
		} else {
			s.Misses = append(s.Misses, e.Value)
		}
		s.Tiers[e.Tier] = ts
	}
	for j, p := range res.Requirements {
		if usedPred[j] {
			continue
		}
		switch {
		case looseMatch(exp.Filler, p.Value) || strictMatch(exp.Filler, p.Value):
			s.FillerHits = append(s.FillerHits, p.Value)
		case looseMatch(exp.Acceptable, p.Value) || strictMatch(exp.Acceptable, p.Value):
			s.AcceptableHits = append(s.AcceptableHits, p.Value)
		case slices.ContainsFunc(variants, func(v []string) bool { return strictMatch(v, p.Value) || looseMatch(v, p.Value) }):
			s.DuplicateHits = append(s.DuplicateHits, p.Value)
		default:
			s.Extras = append(s.Extras, p.Value)
		}
	}

	s.TierOrder = tierOrder(exp, res, matchedPred)
	s.GroupF1 = groupF1(exp, res, matchedPred)
	return s
}

// tierOf returns a prediction's Tier, deriving it from its Context
// Sentences for results stored before Tier existed; empty when neither is
// available.
func tierOf(res domain.Result, j int) domain.Tier {
	p := res.Requirements[j]
	if p.Tier != "" {
		return p.Tier
	}
	var sections []domain.Section
	for _, ref := range p.Refs {
		if cs, ok := res.Context[ref]; ok {
			sections = append(sections, cs.Section)
		}
	}
	if len(sections) == 0 {
		return ""
	}
	return domain.TierFor(sections)
}

// Precision is loose matches over predictions that are neither acceptable
// nor duplicates.
func (s FixtureScore) Precision() float64 {
	return ratio(s.LooseMatched, s.Predicted-len(s.AcceptableHits)-len(s.DuplicateHits))
}

func tierOrder(exp Expected, res domain.Result, matchedPred []int) *float64 {
	distinct := map[float64]bool{}
	for _, p := range res.Requirements {
		distinct[p.Importance] = true
	}
	if len(distinct) < 2 {
		return nil
	}
	var sum float64
	pairs := 0
	for a := range exp.Requirements {
		for b := range exp.Requirements {
			ra, rb := tierRank[exp.Requirements[a].Tier], tierRank[exp.Requirements[b].Tier]
			if ra <= rb || matchedPred[a] < 0 || matchedPred[b] < 0 {
				continue
			}
			ia, ib := res.Requirements[matchedPred[a]].Importance, res.Requirements[matchedPred[b]].Importance
			pairs++
			switch {
			case ia > ib:
				sum++
			case ia == ib:
				sum += 0.5
			}
		}
	}
	if pairs == 0 {
		return nil
	}
	v := sum / float64(pairs)
	return &v
}

// groupF1 compares unordered same-group pairs of expected values.
func groupF1(exp Expected, res domain.Result, matchedPred []int) *float64 {
	if len(res.AlternativeGroups) == 0 {
		return nil
	}
	pair := func(a, b string) string {
		a, b = normalize(a), normalize(b)
		if a > b {
			a, b = b, a
		}
		return a + "|" + b
	}
	pairsOf := func(groups [][]string) map[string]bool {
		out := map[string]bool{}
		for _, g := range groups {
			for i := range g {
				for j := i + 1; j < len(g); j++ {
					out[pair(g[i], g[j])] = true
				}
			}
		}
		return out
	}

	// Map predicted IDs to the expected value they matched.
	idToExpected := map[string]string{}
	for i, j := range matchedPred {
		if j >= 0 {
			idToExpected[res.Requirements[j].ID] = exp.Requirements[i].Value
		}
	}
	var predGroups [][]string
	for _, g := range res.AlternativeGroups {
		var vals []string
		for _, id := range g.Members {
			if v, ok := idToExpected[id]; ok {
				vals = append(vals, v)
			}
		}
		predGroups = append(predGroups, vals)
	}

	want, got := pairsOf(exp.AlternativeGroups), pairsOf(predGroups)
	tp := 0
	for p := range got {
		if want[p] {
			tp++
		}
	}
	var f1 float64
	if tp > 0 {
		precision := float64(tp) / float64(len(got))
		recall := float64(tp) / float64(len(want))
		f1 = 2 * precision * recall / (precision + recall)
	}
	return &f1
}
