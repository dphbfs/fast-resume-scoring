package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev/jevtest"
	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

func newTestChecker(t *testing.T, srvURL string, cfg config.Checker) (*Checker, *metrics.Recorder) {
	t.Helper()
	m := metrics.NewRecorder()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := jev.New(config.Jev{
		APIKey: "k", BaseURL: srvURL, Model: "jev-latest",
		MaxConcurrency: 4, MaxRetries: 0, Timeout: 5 * time.Second,
	}, m, log)
	if err != nil {
		t.Fatal(err)
	}
	return NewChecker(c, m, log, cfg), m
}

// fakeEvidence stands in for Jev: retrieval[statement prefix] gives the
// Retrieval probabilities, strength[statement prefix][requirement] the
// Strength probabilities (none 1.0 when unlisted).
type fakeEvidence struct {
	t         *testing.T
	retrieval map[string]map[string]float64
	strength  map[string]map[string]map[string]float64
}

func (f fakeEvidence) lookup(statement string) string {
	for k := range f.retrieval {
		if strings.HasPrefix(statement, k) {
			return k
		}
	}
	f.t.Errorf("unexpected statement %q", statement)
	return ""
}

func (f fakeEvidence) respond(_ int, req jev.WireRequest) jevtest.Reply {
	state, _ := req.State.(map[string]any)
	statement, _ := state["statement"].(string)
	key := f.lookup(statement)
	answers := map[string]jev.WireAnswer{}
	for id, q := range req.Questions {
		if id == "retrieval" {
			crit := q.Criteria.(map[string]any)
			if _, ok := crit[noneOption]; !ok {
				f.t.Errorf("retrieval question has no %q option", noneOption)
			}
			probs := f.retrieval[key]
			for o := range probs {
				if _, ok := crit[o]; !ok {
					f.t.Errorf("retrieval: %q is not an option", o)
				}
			}
			answers[id] = jevtest.Choice(probs, 0.5)
			continue
		}
		inst := q.Instructions.(map[string]any)
		req, _ := inst["requirement"].(string)
		probs, ok := f.strength[key][req]
		if !ok {
			probs = map[string]float64{"none": 1}
		}
		answers[id] = jevtest.Choice(probs, 0.5)
	}
	return jevtest.Reply{Answers: answers}
}

func checkInput() domain.Result {
	return domain.Result{
		Requirements: []domain.Requirement{
			{ID: "req_1", Value: "Go", Refs: []domain.Ref{"s1", "s2"}, Tier: domain.TierRequired},
			{ID: "req_2", Value: "Kubernetes", Refs: []domain.Ref{"s1"}, Tier: domain.TierPreferred},
			{ID: "req_3", Value: "Ruby", Refs: []domain.Ref{"s2"}, Tier: domain.TierRequired},
		},
		AlternativeGroups: []domain.AlternativeGroup{{ID: "alt_1", Members: []string{"req_1", "req_3"}}},
		Context: map[domain.Ref]domain.ContextSentence{
			"s1": {Text: "Experience with Go services on Kubernetes in production.", Section: domain.SectionRequired},
			"s2": {Text: "Go or Ruby.", Section: domain.SectionRequired},
		},
	}
}

const checkResume = `Jane Doe

# Experience
## Engineer | Acme | 2020 – 2024
- Built Go services on EKS
- Wrote a team newsletter

# Skills
- Go, Kubernetes
`

func TestCheck(t *testing.T) {
	f := fakeEvidence{t: t,
		retrieval: map[string]map[string]float64{
			"Built Go":   {"Go": 0.5, "Kubernetes": 0.4, "Ruby": 0.01, "none": 0.09},
			"Wrote a":    {"none": 0.97, "Go": 0.01, "Kubernetes": 0.01, "Ruby": 0.01},
			"Go, Kubern": {"Go": 0.45, "Kubernetes": 0.45, "none": 0.1},
		},
		strength: map[string]map[string]map[string]float64{
			"Built Go": {
				"Go":         {"strong": 0.8, "partial": 0.1, "weak": 0.05, "none": 0.05},
				"Kubernetes": {"strong": 0.2, "partial": 0.5, "weak": 0, "none": 0.3},
			},
			"Go, Kubern": {
				"Go":         {"strong": 0.9, "none": 0.1},
				"Kubernetes": {"weak": 0.4, "none": 0.6},
			},
		},
	}
	srv := jevtest.NewServer(t, f.respond)
	c, _ := newTestChecker(t, srv.URL, config.Checker{})

	res, trace, err := c.Check(context.Background(), checkInput(), domain.Resume{Text: checkResume})
	if err != nil {
		t.Fatal(err)
	}

	if res.SchemaVersion != domain.CoverageSchemaVersion || res.Model != jevtest.Model {
		t.Errorf("header = %q %q", res.SchemaVersion, res.Model)
	}
	got := map[string]domain.RequirementCoverage{}
	for _, r := range res.Requirements {
		got[r.Value] = r
	}
	goCov := got["Go"]
	if goCov.Coverage != domain.StrengthStrong || goCov.Tier != domain.TierRequired {
		t.Errorf("Go = %+v, want strong required", goCov)
	}
	// Strongest link first; the Skills link is capped at weak.
	if len(goCov.Evidence) != 2 || goCov.Evidence[0].Unit != "e1" || goCov.Evidence[0].Strength != domain.StrengthStrong ||
		math.Abs(goCov.Evidence[0].P-0.95) > 1e-9 ||
		goCov.Evidence[1].Unit != "e3" || goCov.Evidence[1].Strength != domain.StrengthWeak {
		t.Errorf("Go evidence = %+v", goCov.Evidence)
	}
	// Mass 0.7 >= 0.5: linked as partial (argmax of strong/partial/weak).
	if k := got["Kubernetes"]; k.Coverage != domain.StrengthPartial || len(k.Evidence) != 1 {
		t.Errorf("Kubernetes = %+v, want partial from e1 only (skills mass 0.4 rejected)", k)
	}
	if r := got["Ruby"]; r.Coverage != domain.StrengthNone || r.Evidence == nil || len(r.Evidence) != 0 {
		t.Errorf("Ruby = %+v, want none with empty evidence", r)
	}
	if len(res.AlternativeGroups) != 1 || res.AlternativeGroups[0].Coverage != domain.StrengthStrong {
		t.Errorf("groups = %+v, want alt_1 strong (best member)", res.AlternativeGroups)
	}
	// alt_1 required strong (3 × 1) + Kubernetes preferred partial (1.5 × 0.6) = 3.9 of 4.5.
	if fit := res.Fit; fit.Score == nil || *fit.Score != 87 || fit.ByTier[domain.TierPreferred] != 60 || len(fit.Gaps) != 0 {
		t.Errorf("fit = %+v, want 87, preferred 60, no gaps", fit)
	}
	if _, ok := res.EvidenceUnits["e2"]; ok || len(res.EvidenceUnits) != 2 {
		t.Errorf("evidence units = %v, want e1 and e3 only", res.EvidenceUnits)
	}
	if u := res.EvidenceUnits["e1"]; u.Company != "Acme" || u.ResumeSection != domain.ResumeExperience {
		t.Errorf("e1 = %+v", u)
	}

	// 3 retrieval requests + 2 strength requests (the newsletter unit
	// retrieves nothing above the floor).
	if n := len(srv.Requests()); n != 5 {
		t.Errorf("requests = %d, want 5", n)
	}
	if len(trace.Units) != 3 || !slices.Equal(trace.Units[1].Retrieved, []string{}) {
		t.Fatalf("trace units = %+v", trace.Units)
	}
	if p := trace.Units[2].Pairs; len(p) != 2 || !p[0].Capped || p[1].Linked {
		t.Errorf("skills pairs = %+v, want Go capped and Kubernetes rejected", p)
	}
}

func TestCheckQuestionsCarryContext(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(id string, q jev.WireQuestion) jev.WireAnswer {
		if id == "retrieval" {
			return jevtest.Choice(map[string]float64{"Go": 0.9, "none": 0.1}, 0.9)
		}
		return jevtest.Choice(map[string]float64{"strong": 1}, 0.9)
	}))
	c, _ := newTestChecker(t, srv.URL, config.Checker{})
	if _, _, err := c.Check(context.Background(), checkInput(), domain.Resume{Text: "- Built Go services\n"}); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	crit := reqs[0].Questions["retrieval"].Criteria.(map[string]any)
	// The shortest Context Sentence describes the option.
	if crit["Go"] != "Job posting: Go or Ruby." {
		t.Errorf("Go option = %v", crit["Go"])
	}
	inst := reqs[1].Questions["req_0"].Instructions.(map[string]any)
	if inst["requirement"] != "Go" || inst["job_posting_context"] != "Go or Ruby." {
		t.Errorf("strength instructions = %v", inst)
	}
	if reqs[1].State.(map[string]any)["statement"] != "Built Go services" {
		t.Errorf("state = %v", reqs[1].State)
	}
}

func TestRetrieveKeepsTopKAboveFloor(t *testing.T) {
	var reqs []domain.Requirement
	probs := map[string]float64{"none": 0.3}
	for i, v := range []string{"A", "B", "C", "D"} {
		reqs = append(reqs, domain.Requirement{ID: v, Value: v})
		probs[v] = []float64{0.4, 0.2, 0.09, 0.01}[i]
	}
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(string, jev.WireQuestion) jev.WireAnswer {
		return jevtest.Choice(probs, 0.5)
	}))
	c, _ := newTestChecker(t, srv.URL, config.Checker{RetrievalK: 2, RetrievalFloor: 0.05})
	creqs := checkRequirements(domain.Result{Requirements: reqs})
	got, top, _, err := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, creqs)
	if err != nil {
		t.Fatal(err)
	}
	// none (0.3) ranks second but is skipped; K=2 stops before C.
	if !slices.Equal(got, []int{0, 1}) {
		t.Errorf("retrieved = %v, want [0 1]", got)
	}
	if len(top) != 1 || len(top[0].Top) != 5 || top[0].Top[1].Option != "none" || !slices.Equal(top[0].Kept, []string{"A", "B"}) {
		t.Errorf("trace rounds = %+v", top)
	}

	c, _ = newTestChecker(t, srv.URL, config.Checker{RetrievalK: 5, RetrievalFloor: 0.05})
	if got, _, _, _ := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, creqs); !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("retrieved = %v, want [0 1 2] (D below floor)", got)
	}
}

func TestCheckRequirementsMakesOptionsUnique(t *testing.T) {
	creqs := checkRequirements(domain.Result{Requirements: []domain.Requirement{
		{ID: "req_1", Value: "Go"}, {ID: "req_2", Value: "go"}, {ID: "req_3", Value: "None"},
	}})
	got := []string{creqs[0].option, creqs[1].option, creqs[2].option}
	want := []string{"Go", "go (req_2)", "None (req_3)"}
	if !slices.Equal(got, want) {
		t.Errorf("options = %q, want %q", got, want)
	}
}

// softmaxOver answers a Retrieval Choice like a softmax over fixed scores:
// the offered options' scores renormalized to sum to 1.
func softmaxOver(scores map[string]float64) jevtest.Responder {
	return jevtest.AnswerAll(func(_ string, q jev.WireQuestion) jev.WireAnswer {
		crit := q.Criteria.(map[string]any)
		var sum float64
		for o := range crit {
			sum += scores[o]
		}
		probs := map[string]float64{}
		for o := range crit {
			probs[o] = scores[o] / sum
		}
		return jevtest.Choice(probs, 0.5)
	})
}

func abcdRequirements() []checkRequirement {
	var reqs []domain.Requirement
	for _, v := range []string{"A", "B", "C", "D"} {
		reqs = append(reqs, domain.Requirement{ID: v, Value: v})
	}
	return checkRequirements(domain.Result{Requirements: reqs})
}

func TestRetrievePeel(t *testing.T) {
	scores := map[string]float64{"A": 0.5, "B": 0.25, "C": 0.12, "D": 0.01, "none": 0.05}
	srv := jevtest.NewServer(t, softmaxOver(scores))
	c, _ := newTestChecker(t, srv.URL, config.Checker{RetrievalMode: "peel", RetrievalK: 5})
	got, rounds, _, err := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, abcdRequirements())
	if err != nil {
		t.Fatal(err)
	}
	// A, B and C each win a round once the winners above them are removed;
	// then none (0.05) beats D (0.01) and the loop ends.
	if !slices.Equal(got, []int{0, 1, 2}) || len(rounds) != 4 {
		t.Errorf("retrieved = %v in %d rounds, want [0 1 2] in 4", got, len(rounds))
	}
	if n := rounds[3].Options; n != 2 {
		t.Errorf("last round offered %d options, want D + none", n)
	}

	c, _ = newTestChecker(t, srv.URL, config.Checker{RetrievalMode: "peel", RetrievalK: 2, PeelShortlist: 3})
	got, rounds, _, _ = c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, abcdRequirements())
	// The shortlist round keeps A, B, C; K=2 stops after two peels.
	if !slices.Equal(got, []int{0, 1}) || len(rounds) != 3 || !slices.Equal(rounds[0].Kept, []string{"A", "B", "C"}) {
		t.Errorf("shortlist peel = %v, rounds %+v", got, rounds)
	}
}

func TestRetrieveNarrow(t *testing.T) {
	// Ten filler Requirements hold 0.2 of the mass in the first round, which
	// keeps C (0.035) under the 0.04 floor. Narrowing to the best 3 drops
	// them, and C's share in the final round rises to 0.035/0.805 = 0.043.
	reqs := []domain.Requirement{{ID: "A", Value: "A"}, {ID: "B", Value: "B"}, {ID: "C", Value: "C"}}
	scores := map[string]float64{"A": 0.5, "B": 0.2, "C": 0.035, "none": 0.07}
	for i := range 10 {
		v := fmt.Sprintf("F%d", i)
		reqs = append(reqs, domain.Requirement{ID: v, Value: v})
		scores[v] = 0.02
	}
	creqs := checkRequirements(domain.Result{Requirements: reqs})
	srv := jevtest.NewServer(t, softmaxOver(scores))

	c, _ := newTestChecker(t, srv.URL, config.Checker{RetrievalMode: "single", RetrievalK: 5, RetrievalFloor: 0.04})
	if got, _, _, _ := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, creqs); !slices.Equal(got, []int{0, 1}) {
		t.Errorf("single = %v, want [0 1]", got)
	}

	c, _ = newTestChecker(t, srv.URL, config.Checker{RetrievalMode: "narrow", NarrowSizes: []int{3}, RetrievalK: 5, RetrievalFloor: 0.04})
	got, rounds, _, err := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, creqs)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{0, 1, 2}) || len(rounds) != 2 || rounds[1].Options != 4 {
		t.Errorf("narrow = %v, rounds %+v; want [0 1 2] over 2 rounds", got, rounds)
	}
}

func TestStrengthNegativesRejectWithReason(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(id string, q jev.WireQuestion) jev.WireAnswer {
		if id == "retrieval" {
			return jevtest.Choice(map[string]float64{"Go": 0.5, "Kubernetes": 0.4, "none": 0.1}, 0.5)
		}
		crit := q.Criteria.(map[string]any)
		for _, neg := range []string{"alternative_tool", "shared_words_only", "different_skill", "context_only", "none"} {
			if _, ok := crit[neg]; !ok {
				t.Errorf("v3 criteria lack %q", neg)
			}
		}
		if q.Instructions.(map[string]any)["requirement"] == "Kubernetes" {
			return jevtest.Choice(map[string]float64{"alternative_tool": 0.6, "partial": 0.3, "none": 0.1}, 0.5)
		}
		return jevtest.Choice(map[string]float64{"strong": 0.9, "different_skill": 0.1}, 0.5)
	}))
	c, m := newTestChecker(t, srv.URL, config.Checker{StrengthCriteria: "v3"})
	res, trace, err := c.Check(context.Background(), checkInput(), domain.Resume{Text: "- Ran Go services on Nomad\n"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requirements[0].Coverage != domain.StrengthStrong || res.Requirements[1].Coverage != domain.StrengthNone {
		t.Errorf("coverage = %+v", res.Requirements)
	}
	p := trace.Units[0].Pairs
	if len(p) != 2 || p[1].Linked || p[1].RejectReason != "alternative_tool" {
		t.Errorf("pairs = %+v, want Kubernetes rejected as alternative_tool", p)
	}
	if m.Summary().Counters["checker.strength.rejected.alternative_tool"] != 1 {
		t.Errorf("counters = %v", m.Summary().Counters)
	}
}

func TestRetrieveNoul(t *testing.T) {
	// Independent probabilities: four Requirements can all clear 0.5, which
	// a single Choice (one softmax) cannot express.
	yes := map[string]float64{"A": 0.9, "B": 0.8, "C": 0.6, "D": 0.2}
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(_ string, q jev.WireQuestion) jev.WireAnswer {
		if q.Type != "noul" {
			t.Errorf("question type = %q, want noul", q.Type)
		}
		inst := q.Instructions.(map[string]any)
		return jevtest.Noul(yes[inst["requirement"].(string)])
	}))
	c, m := newTestChecker(t, srv.URL, config.Checker{RetrievalMode: "noul", RetrievalK: 5, NoulThreshold: 0.5})
	got, rounds, _, err := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, abcdRequirements())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("retrieved = %v, want [0 1 2] (D below threshold)", got)
	}
	if len(srv.Requests()) != 1 || len(srv.Requests()[0].Questions) != 4 {
		t.Errorf("want one request with 4 nouls, got %+v", srv.Requests())
	}
	if len(rounds) != 1 || rounds[0].Top[0].Option != "A" || !slices.Equal(rounds[0].Kept, []string{"A", "B", "C"}) {
		t.Errorf("rounds = %+v", rounds)
	}
	if m.Summary().Counters["checker.retrieval.requests"] != 1 {
		t.Errorf("counters = %v", m.Summary().Counters)
	}

	c, _ = newTestChecker(t, srv.URL, config.Checker{RetrievalMode: "noul", RetrievalK: 2, NoulThreshold: 0.5})
	if got, _, _, _ := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, abcdRequirements()); !slices.Equal(got, []int{0, 1}) {
		t.Errorf("K=2: retrieved = %v, want [0 1]", got)
	}
}

func TestStrengthCriteriaVersions(t *testing.T) {
	for _, tt := range []struct {
		version string
		has     []string
		lacks   []string
	}{
		{"v1", []string{"strong", "none"}, []string{"alternative_tool", "needed_capability"}},
		{"v3", []string{"alternative_tool", "context_only"}, []string{"needed_capability"}},
		{"v4", []string{"alternative_tool", "needed_capability"}, nil},
		{"", []string{"alternative_tool"}, []string{"needed_capability"}}, // default v3
	} {
		crit := (&Checker{cfg: config.Checker{StrengthCriteria: tt.version}}).strengthCriteria()
		for _, o := range tt.has {
			if _, ok := crit[o]; !ok {
				t.Errorf("%q: missing %q", tt.version, o)
			}
		}
		for _, o := range tt.lacks {
			if _, ok := crit[o]; ok {
				t.Errorf("%q: unexpected %q", tt.version, o)
			}
		}
	}
	// v4 must not change the shared v3 map.
	if _, ok := strengthNegatives["needed_capability"]; ok {
		t.Error("v4 leaked into strengthNegatives")
	}
}

func TestStrengthGateDecidesLinks(t *testing.T) {
	// Go: Choice says strong with mass 0.9, but the gate says no (0.2).
	// Kubernetes: mass only 0.3, but the gate says yes (0.8) -> linked,
	// graded by the Choice's argmax (partial).
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(id string, q jev.WireQuestion) jev.WireAnswer {
		if id == "retrieval" {
			return jevtest.Choice(map[string]float64{"Go": 0.5, "Kubernetes": 0.4, "none": 0.1}, 0.5)
		}
		req := q.Instructions.(map[string]any)["requirement"]
		if strings.HasPrefix(id, "gate_") {
			if q.Type != "noul" {
				t.Errorf("%s type = %q, want noul", id, q.Type)
			}
			if req == "Go" {
				return jevtest.Noul(0.2)
			}
			return jevtest.Noul(0.8)
		}
		if req == "Go" {
			return jevtest.Choice(map[string]float64{"strong": 0.9, "none": 0.1}, 0.8)
		}
		return jevtest.Choice(map[string]float64{"partial": 0.3, "alternative_tool": 0.7}, 0.4)
	}))
	c, _ := newTestChecker(t, srv.URL, config.Checker{GateThreshold: 0.5})
	res, trace, err := c.Check(context.Background(), checkInput(), domain.Resume{Text: "- Ran Go services on Nomad\n"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requirements[0].Coverage != domain.StrengthNone || res.Requirements[1].Coverage != domain.StrengthPartial {
		t.Errorf("coverage = Go %s, Kubernetes %s; want none, partial", res.Requirements[0].Coverage, res.Requirements[1].Coverage)
	}
	if q := srv.Requests()[1].Questions; len(q) != 4 {
		t.Errorf("strength request has %d questions, want 2 strength + 2 gates", len(q))
	}
	if p := trace.Units[0].Pairs; p[0].Gate == nil || *p[0].Gate != 0.2 || p[0].Linked {
		t.Errorf("Go pair = %+v", p[0])
	}

	// Gate off: no gate questions, mass rule decides.
	c, _ = newTestChecker(t, srv.URL, config.Checker{})
	res, _, _ = c.Check(context.Background(), checkInput(), domain.Resume{Text: "- Ran Go services on Nomad\n"})
	if res.Requirements[0].Coverage != domain.StrengthStrong || res.Requirements[1].Coverage != domain.StrengthNone {
		t.Errorf("gate off: coverage = %s, %s; want strong, none", res.Requirements[0].Coverage, res.Requirements[1].Coverage)
	}
}

func TestStrengthVeto(t *testing.T) {
	// Both gates pass; Kubernetes's grading Choice puts 0.7 on
	// alternative_tool, Go's puts 0.4 on none.
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(id string, q jev.WireQuestion) jev.WireAnswer {
		if id == "retrieval" {
			return jevtest.Choice(map[string]float64{"Go": 0.5, "Kubernetes": 0.4, "none": 0.1}, 0.5)
		}
		if strings.HasPrefix(id, "gate_") {
			return jevtest.Noul(0.8)
		}
		if q.Instructions.(map[string]any)["requirement"] == "Go" {
			return jevtest.Choice(map[string]float64{"strong": 0.6, "none": 0.4}, 0.6)
		}
		return jevtest.Choice(map[string]float64{"strong": 0.3, "alternative_tool": 0.7}, 0.7)
	}))
	resume := domain.Resume{Text: "- Ran Go services on Nomad\n"}

	c, m := newTestChecker(t, srv.URL, config.Checker{GateThreshold: 0.5, VetoThreshold: 0.6})
	res, trace, err := c.Check(context.Background(), checkInput(), resume)
	if err != nil {
		t.Fatal(err)
	}
	if res.Requirements[0].Coverage != domain.StrengthStrong || res.Requirements[1].Coverage != domain.StrengthNone {
		t.Errorf("coverage = Go %s, Kubernetes %s; want strong, none", res.Requirements[0].Coverage, res.Requirements[1].Coverage)
	}
	for _, p := range trace.Units[0].Pairs {
		if p.Requirement == "Kubernetes" && (!p.Vetoed || p.Linked || p.RejectReason != "alternative_tool") {
			t.Errorf("Kubernetes pair = %+v, want vetoed as alternative_tool", p)
		}
	}
	if m.Summary().Counters["checker.strength.vetoed.alternative_tool"] != 1 {
		t.Errorf("counters = %v", m.Summary().Counters)
	}

	// Veto off (default): the gate alone decides.
	c, _ = newTestChecker(t, srv.URL, config.Checker{GateThreshold: 0.5})
	res, _, _ = c.Check(context.Background(), checkInput(), resume)
	if res.Requirements[1].Coverage != domain.StrengthStrong {
		t.Errorf("veto off: Kubernetes = %s, want strong", res.Requirements[1].Coverage)
	}
}

func TestStrengthCriteriaV6ChangesOnlyAlternativeTool(t *testing.T) {
	v5 := (&Checker{cfg: config.Checker{StrengthCriteria: "v5"}}).strengthCriteria()
	v6 := (&Checker{cfg: config.Checker{StrengthCriteria: "v6"}}).strengthCriteria()
	if len(v6) != len(v5) {
		t.Fatalf("v6 has %d options, want %d", len(v6), len(v5))
	}
	for o := range v5 {
		same := reflect.DeepEqual(v5[o], v6[o])
		if o == "alternative_tool" && same || o != "alternative_tool" && !same {
			t.Errorf("option %q: changed = %v", o, !same)
		}
	}
}

func TestStrengthCriteriaV5IsStructured(t *testing.T) {
	crit := (&Checker{cfg: config.Checker{StrengthCriteria: "v5"}}).strengthCriteria()
	if len(crit) != len(strengthCriteriaV2)+len(strengthNegatives) {
		t.Errorf("v5 has %d options, want the same %d as v3", len(crit), len(strengthCriteriaV2)+len(strengthNegatives))
	}
	for o, d := range crit {
		obj, ok := d.(map[string]any)
		if !ok || obj["what"] == nil {
			t.Errorf("option %q is not a {what, ...} object: %v", o, d)
		}
		if _, inV3 := strengthNegatives[o]; !inV3 && domain.EvidenceStrength(o).Rank() == 0 && o != "none" {
			t.Errorf("option %q is not in v3", o)
		}
	}
}

func TestStrengthScoreMode(t *testing.T) {
	// Score grades, the gate decides: Go level 2 -> strong; Kubernetes
	// level 1 -> partial; both pass the gate.
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(id string, q jev.WireQuestion) jev.WireAnswer {
		if id == "retrieval" {
			return jevtest.Choice(map[string]float64{"Go": 0.5, "Kubernetes": 0.4, "none": 0.1}, 0.5)
		}
		if strings.HasPrefix(id, "gate_") {
			crit := q.Criteria.(map[string]any)
			if !strings.Contains(crit["true"].(string), "prerequisite") {
				t.Errorf("gate v2 wording missing: %v", crit["true"])
			}
			return jevtest.Noul(0.7)
		}
		if q.Type != "score" {
			t.Errorf("%s type = %q, want score", id, q.Type)
		}
		if q.Instructions.(map[string]any)["requirement"] == "Go" {
			return jevtest.Score(1.8, 0.7, map[string]float64{"0": 0.05, "1": 0.1, "2": 0.85})
		}
		return jevtest.Score(1.0, 0.5, map[string]float64{"0": 0.2, "1": 0.6, "2": 0.2})
	}))
	c, _ := newTestChecker(t, srv.URL, config.Checker{StrengthMode: "score", GateThreshold: 0.4, GateWording: "v2"})
	res, _, err := c.Check(context.Background(), checkInput(), domain.Resume{Text: "- Ran Go services on Kubernetes\n"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requirements[0].Coverage != domain.StrengthStrong || res.Requirements[1].Coverage != domain.StrengthPartial {
		t.Errorf("coverage = %s, %s; want strong, partial", res.Requirements[0].Coverage, res.Requirements[1].Coverage)
	}
}

func TestLevelStrength(t *testing.T) {
	tests := []struct {
		probs map[string]float64
		want  domain.EvidenceStrength
	}{
		{map[string]float64{"0": 0.7, "1": 0.2, "2": 0.1}, domain.StrengthWeak},
		{map[string]float64{"0": 0.1, "1": 0.5, "2": 0.4}, domain.StrengthPartial},
		{map[string]float64{"0": 0.1, "1": 0.1, "2": 0.8}, domain.StrengthStrong},
	}
	for _, tt := range tests {
		if got := levelStrength(tt.probs); got != tt.want {
			t.Errorf("levelStrength(%v) = %s, want %s", tt.probs, got, tt.want)
		}
	}
}

// gateFake answers retrieval with Go and Kubernetes, gates Go no (0.2) and
// Kubernetes yes (0.8), and grades every pair strong.
func gateFake(t *testing.T) *jevtest.Server {
	return jevtest.NewServer(t, jevtest.AnswerAll(func(id string, q jev.WireQuestion) jev.WireAnswer {
		if id == "retrieval" {
			return jevtest.Choice(map[string]float64{"Go": 0.5, "Kubernetes": 0.4, "none": 0.1}, 0.5)
		}
		if strings.HasPrefix(id, "gate_") {
			if q.Instructions.(map[string]any)["requirement"] == "Go" {
				return jevtest.Noul(0.2)
			}
			return jevtest.Noul(0.8)
		}
		return jevtest.Choice(map[string]float64{"strong": 0.9, "none": 0.1}, 0.8)
	}))
}

// strengthQuestions lists the question IDs of each Strength request.
func strengthQuestions(srv *jevtest.Server) [][]string {
	var out [][]string
	for _, r := range srv.Requests() {
		if _, ok := r.Questions["retrieval"]; ok {
			continue
		}
		out = append(out, slices.Sorted(maps.Keys(r.Questions)))
	}
	return out
}

func TestStrengthGateFirstGradesOnlyPassedPairs(t *testing.T) {
	srv := gateFake(t)
	c, m := newTestChecker(t, srv.URL, config.Checker{GateThreshold: 0.5, GateFirst: true})
	res, trace, err := c.Check(context.Background(), checkInput(), domain.Resume{Text: "- Ran Go services on Nomad\n"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requirements[0].Coverage != domain.StrengthNone || res.Requirements[1].Coverage != domain.StrengthStrong {
		t.Errorf("coverage = Go %s, Kubernetes %s; want none, strong", res.Requirements[0].Coverage, res.Requirements[1].Coverage)
	}
	// Retrieval order: Go is pair 0, Kubernetes pair 1.
	want := [][]string{{"gate_0", "gate_1"}, {"req_1"}}
	if got := strengthQuestions(srv); !reflect.DeepEqual(got, want) {
		t.Errorf("strength requests = %v, want %v", got, want)
	}
	if p := trace.Units[0].Pairs[0]; p.Linked || p.RejectReason != "gate" || p.Probabilities != nil {
		t.Errorf("Go pair = %+v, want rejected by the gate, ungraded", p)
	}
	if n := m.Summary().Counters["checker.strength.requests"]; n != 2 {
		t.Errorf("strength requests = %d, want 2", n)
	}
}

func TestStrengthSkipCappedGrading(t *testing.T) {
	srv := gateFake(t)
	c, _ := newTestChecker(t, srv.URL, config.Checker{GateThreshold: 0.5, SkipCappedGrading: true})
	resume := "# Skills\n- Go, Kubernetes\n\n# Experience\n## Engineer | Acme | 2020 – 2024\n- Ran Go services on Nomad\n"
	res, trace, err := c.Check(context.Background(), checkInput(), domain.Resume{Text: resume})
	if err != nil {
		t.Fatal(err)
	}
	// Skills unit: gates only, Kubernetes linked weak on the gate's P.
	// Experience unit: gates and grades in one request.
	want := [][]string{{"gate_0", "gate_1"}, {"gate_0", "gate_1", "req_0", "req_1"}}
	got := strengthQuestions(srv)
	slices.SortFunc(got, func(a, b []string) int { return len(a) - len(b) })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("strength requests = %v, want %v", got, want)
	}
	if res.Requirements[1].Coverage != domain.StrengthStrong {
		t.Errorf("Kubernetes coverage = %s, want strong (experience unit)", res.Requirements[1].Coverage)
	}
	var skills domain.TraceUnit
	for _, u := range trace.Units {
		if u.ResumeSection == domain.ResumeSkills {
			skills = u
		}
	}
	for _, p := range skills.Pairs {
		if p.Requirement == "Kubernetes" && (!p.Linked || p.Strength != domain.StrengthWeak || p.EvidenceMass != 0) {
			t.Errorf("skills Kubernetes pair = %+v, want linked weak, ungraded", p)
		}
	}
	for _, r := range res.Requirements {
		for _, l := range r.Evidence {
			if l.Strength == domain.StrengthWeak && l.P != 0.8 {
				t.Errorf("%s weak link P = %v, want the gate's 0.8", r.Value, l.P)
			}
		}
	}
}

func TestRetrieveNarrowStopsEarly(t *testing.T) {
	reqs := []domain.Requirement{{ID: "A", Value: "A"}, {ID: "B", Value: "B"}, {ID: "C", Value: "C"}}
	creqs := checkRequirements(domain.Result{Requirements: reqs})
	srv := jevtest.NewServer(t, softmaxOver(map[string]float64{"A": 0.995, "B": 0.004, "none": 0.001}))
	c, m := newTestChecker(t, srv.URL, config.Checker{RetrievalMode: "narrow", NarrowSizes: []int{2}, RetrievalK: 5, RetrievalFloor: 0.01, NarrowStopP: 0.99})
	got, rounds, _, err := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, creqs)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{0}) || len(rounds) != 1 || m.Summary().Counters["checker.retrieval.stopped_early"] != 1 {
		t.Errorf("retrieved %v over %d rounds, want [0] after 1 round", got, len(rounds))
	}
}
