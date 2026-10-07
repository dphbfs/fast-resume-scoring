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

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev/jevtest"
	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-scoring/tuning"
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
	return NewChecker(c, m, log, withTestDefaults(cfg), tuning.Default()), m
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

	c, _ := newTestChecker(t, srv.URL, config.Checker{RetrievalK: 5, RetrievalFloor: 0.04})
	if got, _, _, _ := c.retrieve(context.Background(), domain.EvidenceUnit{Text: "x"}, creqs); !slices.Equal(got, []int{0, 1}) {
		t.Errorf("one round = %v, want [0 1]", got)
	}

	c, _ = newTestChecker(t, srv.URL, config.Checker{NarrowSizes: []int{3}, RetrievalK: 5, RetrievalFloor: 0.04})
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
				t.Errorf("criteria lack %q", neg)
			}
		}
		if q.Instructions.(map[string]any)["requirement"] == "Kubernetes" {
			return jevtest.Choice(map[string]float64{"alternative_tool": 0.6, "partial": 0.3, "none": 0.1}, 0.5)
		}
		return jevtest.Choice(map[string]float64{"strong": 0.9, "different_skill": 0.1}, 0.5)
	}))
	c, m := newTestChecker(t, srv.URL, config.Checker{})
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

func TestStrengthCriteriaAreStructured(t *testing.T) {
	want := []string{"strong", "partial", "weak", "none", "alternative_tool", "shared_words_only", "different_skill", "context_only"}
	criteria := newPrompts(tuning.Default()).strengthCriteria
	if got := slices.Sorted(maps.Keys(criteria)); !slices.Equal(got, slices.Sorted(slices.Values(want))) {
		t.Errorf("options = %v, want %v", got, want)
	}
	for o, d := range criteria {
		if obj, ok := d.(map[string]any); !ok || obj["what"] == nil {
			t.Errorf("option %q is not a {what, ...} object: %v", o, d)
		}
	}
}

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

func TestWithoutMentioned(t *testing.T) {
	in := domain.Result{
		Requirements: []domain.Requirement{
			{ID: "a", Tier: domain.TierRequired}, {ID: "b", Tier: domain.TierMentioned},
			{ID: "c", Tier: domain.TierPreferred}, {ID: "d", Tier: domain.TierMentioned},
		},
		AlternativeGroups: []domain.AlternativeGroup{{ID: "g1", Members: []string{"a", "c", "d"}}, {ID: "g2", Members: []string{"a", "b"}}},
	}
	out := withoutMentioned(in)
	var ids []string
	for _, r := range out.Requirements {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []string{"a", "c"}) {
		t.Errorf("requirements = %v", ids)
	}
	if len(out.AlternativeGroups) != 1 || !slices.Equal(out.AlternativeGroups[0].Members, []string{"a", "c"}) {
		t.Errorf("groups = %+v", out.AlternativeGroups)
	}
	if len(in.Requirements) != 4 || len(in.AlternativeGroups[0].Members) != 3 {
		t.Error("input was modified")
	}
}

// withTestDefaults fills the zero numeric settings with the values these
// tests were written against (K 5, floor 0.02, mass 0.5); the switches
// stay as given, so a zero Checker means one retrieval round and no gate.
func withTestDefaults(cfg config.Checker) config.Checker {
	if cfg.RetrievalK == 0 {
		cfg.RetrievalK = 5
	}
	if cfg.RetrievalFloor == 0 {
		cfg.RetrievalFloor = 0.02
	}
	if cfg.MinEvidenceMass == 0 {
		cfg.MinEvidenceMass = 0.5
	}
	return cfg
}
