package app

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev/jevtest"
	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
)

// questionRequirement returns the requirement embedded in a Refinement question.
func questionRequirement(t *testing.T, q jev.WireQuestion) string {
	t.Helper()
	inst, _ := q.Instructions.(map[string]any)
	v, ok := inst["requirement"].(string)
	if !ok {
		t.Fatalf("question has no embedded requirement: %+v", q.Instructions)
	}
	return v
}

func refinementRun() *run {
	return &run{
		jd:      domain.JobDescription{Title: "Backend Engineer"},
		summary: "A backend role.",
		sentences: []domain.ContextSentence{
			{Ref: "s1", Text: "Kubernetes in production.", Section: domain.SectionRequired},
			{Ref: "s2", Text: "We run K8s everywhere.", Section: domain.SectionResponsibilities},
			{Ref: "s3", Text: "Go or Ruby.", Section: domain.SectionRequired},
			{Ref: "s4", Text: "A team player.", Section: domain.SectionRequired},
			{Ref: "s5", Text: "Bonus: Kafka.", Section: domain.SectionPreferred},
		},
		accepted: []judged{
			{Text: "Kubernetes", Ref: "s1"},
			{Text: "K8s", Ref: "s2"},
			{Text: "Go", Ref: "s3"},
			{Text: "Ruby", Ref: "s3"},
			{Text: "team player", Ref: "s4"},
			{Text: "Kafka", Ref: "s5"},
		},
	}
}

// refinementResponder stands in for Jev on the Refinement Round.
func refinementResponder(t *testing.T) jevtest.Responder {
	t.Helper()
	scores := map[string]float64{"Kubernetes": 3, "K8s": 1, "Go": 3, "Ruby": 3, "team player": 3, "Kafka": 2}
	return func(_ int, req jev.WireRequest) jevtest.Reply {
		answers := map[string]jev.WireAnswer{}
		for id, q := range req.Questions {
			v := questionRequirement(t, q)
			crit, _ := q.Criteria.(map[string]any)
			switch {
			case strings.HasPrefix(id, "filler_"):
				kind := "specific_requirement"
				if v == "team player" {
					kind = "generic_trait"
				}
				answers[id] = jevtest.Choice(map[string]float64{kind: 0.9, "vague_term": 0.1}, 0.8)
			case strings.HasPrefix(id, "dup_"):
				pick := map[string]string{"K8s": "Kubernetes", "Kubernetes": "K8s", "Kafka": "Go"}[v]
				if pick == "" {
					pick = "different_thing"
				}
				if _, ok := crit[pick]; !ok {
					t.Errorf("dup question for %q lacks option %q", v, pick)
				}
				answers[id] = jevtest.Choice(map[string]float64{pick: 0.9, "broader_or_narrower": 0.1}, 0.8)
			case strings.HasPrefix(id, "alt_"):
				pick := map[string]string{"Go": "Ruby", "Ruby": "Go"}[v]
				if pick == "" {
					t.Errorf("alt question asked for %q, which shares no sentence with another requirement", v)
					pick = "unrelated"
				}
				answers[id] = jevtest.Choice(map[string]float64{pick: 0.9, "required_together": 0.1}, 0.8)
			case strings.HasPrefix(id, "importance_"):
				s := scores[v]
				answers[id] = jevtest.Score(s, 0.9, map[string]float64{})
			default:
				t.Errorf("unexpected question id %q", id)
			}
		}
		return jevtest.Reply{Answers: answers}
	}
}

func TestRefinementRound(t *testing.T) {
	srv := jevtest.NewServer(t, refinementResponder(t))
	e, m := newTestExtractor(t, srv.URL, config.Pipeline{})

	r := refinementRun()
	if err := e.refinementRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	// Filler dropped; K8s merged into Kubernetes (canonical: first seen).
	got := acceptedTexts(r.accepted)
	if !slices.Equal(got, []string{"Kubernetes", "Kubernetes", "Go", "Ruby", "Kafka"}) {
		t.Errorf("accepted = %q", got)
	}
	if r.accepted[1].Ref != "s2" {
		t.Errorf("merged K8s lost its ref: %+v", r.accepted[1])
	}
	// Importance = score/4; merged keeps the max.
	want := map[string]float64{"kubernetes": 0.75, "go": 0.75, "ruby": 0.75, "kafka": 0.5}
	for k, v := range want {
		if r.importance[k] != v {
			t.Errorf("importance[%q] = %v, want %v", k, r.importance[k], v)
		}
	}
	if len(r.groups) != 1 || !slices.Equal(r.groups[0], []string{"Go", "Ruby"}) {
		t.Errorf("groups = %q, want [[Go Ruby]]", r.groups)
	}

	// Questions embed their data: every mention's sentence and section.
	for _, req := range srv.Requests() {
		raw, _ := json.Marshal(req.State)
		if strings.Contains(string(raw), "Kafka") {
			t.Errorf("state carries requirements; they belong in the questions: %s", raw)
		}
		for id, q := range req.Questions {
			if questionRequirement(t, q) == "Kafka" && strings.HasPrefix(id, "importance_") {
				mentions, _ := q.Instructions.(map[string]any)["mentions"].([]any)
				if len(mentions) != 1 || !strings.Contains(mustJSON(mentions[0]), "preferred") {
					t.Errorf("Kafka mentions = %v, want one preferred sentence", mentions)
				}
			}
		}
	}

	tr := map[string]domain.TraceRequirement{}
	for _, x := range r.trace.Refinement {
		tr[x.Value] = x
	}
	if x := tr["team player"]; x.Kept || x.FillerKind != "generic_trait" {
		t.Errorf("trace team player = %+v, want dropped as generic_trait", x)
	}
	if x := tr["K8s"]; x.MergedInto != "Kubernetes" || x.DuplicateOf != "Kubernetes" {
		t.Errorf("trace K8s = %+v, want merged into Kubernetes", x)
	}
	if x := tr["Kafka"]; x.DuplicateOf != "Go" || x.MergedInto != "" || x.Score != 2 || x.Importance != 0.5 {
		t.Errorf("trace Kafka = %+v, want one-way duplicate of Go, score 2", x)
	}
	if x := tr["Go"]; x.AlternativeOf != "Ruby" {
		t.Errorf("trace Go = %+v, want alternative of Ruby", x)
	}
	if len(r.trace.Merges) != 1 || r.trace.Merges[0] != [2]string{"Kubernetes", "K8s"} || len(r.trace.Groups) != 1 {
		t.Errorf("trace merges = %v, groups = %v", r.trace.Merges, r.trace.Groups)
	}

	c := m.Summary().Counters
	// Kafka -> Go is one-way, so it does not merge.
	if c["refinement.merged"] != 1 || c["refinement.one_way_duplicate"] != 1 ||
		c["refinement.dropped.generic_trait"] != 1 || c["refinement.alternative_links"] != 2 {
		t.Errorf("counters = %v", c)
	}
}

func TestRefinementRoundSkipImportance(t *testing.T) {
	srv := jevtest.NewServer(t, refinementResponder(t))
	e, _ := newTestExtractor(t, srv.URL, config.Pipeline{SkipImportance: true})
	r := refinementRun()
	if err := e.refinementRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for _, req := range srv.Requests() {
		for id := range req.Questions {
			if strings.HasPrefix(id, "importance_") {
				t.Errorf("asked %s with SkipImportance", id)
			}
		}
	}
	// Same Requirements kept and merged as with Importance; all at 0.
	if got := acceptedTexts(r.accepted); !slices.Equal(got, []string{"Kubernetes", "Kubernetes", "Go", "Ruby", "Kafka"}) {
		t.Errorf("accepted = %q", got)
	}
	want := map[string]float64{"kubernetes": 0, "go": 0, "ruby": 0, "kafka": 0}
	if !maps.Equal(r.importance, want) {
		t.Errorf("importance = %v, want %v", r.importance, want)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestRefinementRoundBatchesBySize(t *testing.T) {
	srv := jevtest.NewServer(t, refinementResponder(t))
	e, _ := newTestExtractor(t, srv.URL, config.Pipeline{})
	e.refinementBatchChars = 1 // forces one question per request
	r := refinementRun()
	if err := e.refinementRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for _, req := range srv.Requests() {
		if len(req.Questions) != 1 {
			t.Fatalf("request has %d questions, want 1", len(req.Questions))
		}
	}
}

func TestDuplicateOptions(t *testing.T) {
	values := []string{"PostgreSQL", "Postgres", "Kafka", "Kafka Streams", "Go", "AWS"}
	// Small lists offer every other value.
	if got := duplicateOptions(values, 0, 40); len(got) != 5 || slices.Contains(got, "PostgreSQL") {
		t.Errorf("small list options = %q", got)
	}
	// Large lists offer only similar values.
	got := duplicateOptions(values, 2, 1)
	if !slices.Equal(got, []string{"Kafka Streams"}) {
		t.Errorf("similar options for Kafka = %q, want [Kafka Streams]", got)
	}
	if got := duplicateOptions(values, 0, 1); !slices.Contains(got, "Postgres") {
		t.Errorf("similar options for PostgreSQL = %q, want Postgres", got)
	}
}

func TestIsJobTitleFragment(t *testing.T) {
	title := "Senior Software Engineer, PHP"
	tests := map[string]bool{
		"Senior Software Engineer, PHP": true, // the title
		"Software Engineer PHP":         true, // run of the title with a role word
		"Senior Software Engineer":      true,
		"PHP":                           false, // one word: a real skill
		"Software":                      false,
		"PHP frameworks":                false, // not in the title
	}
	for v, want := range tests {
		if got := isJobTitleFragment(v, title); got != want {
			t.Errorf("isJobTitleFragment(%q) = %v, want %v", v, got, want)
		}
	}
	if isJobTitleFragment("Golang services", "Senior Software Engineer -Golang (Security)") {
		t.Error("a phrase without a role word is not a title fragment")
	}
}

func TestRefinementRoundDropsJobTitle(t *testing.T) {
	srv := jevtest.NewServer(t, refinementResponder(t))
	e, m := newTestExtractor(t, srv.URL, config.Pipeline{})
	r := refinementRun()
	r.jd.Title = "Senior Backend Engineer"
	r.accepted = append(r.accepted, judged{Text: "Backend Engineer", Ref: "s1"})
	if err := e.refinementRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(acceptedTexts(r.accepted), "Backend Engineer") {
		t.Errorf("accepted = %q, want the job title dropped", acceptedTexts(r.accepted))
	}
	if m.Summary().Counters["refinement.dropped.job_title"] != 1 {
		t.Error("job title drop not counted")
	}
	for _, req := range srv.Requests() {
		for _, q := range req.Questions {
			if questionRequirement(t, q) == "Backend Engineer" {
				t.Fatal("job title was sent to Jev")
			}
		}
	}
}
