package app

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

var updateSnapshot = flag.Bool("update", false, "rewrite testdata/prompts.golden.json")

// TestPromptSnapshot pins the exact JSON of every question and prompt the
// app sends. Several wordings are fitted constants (docs/adr/0004), so a
// change here must be deliberate: rerun with -update and review the diff.
func TestPromptSnapshot(t *testing.T) {
	m := []mention{{Section: domain.SectionRequired, Sentence: "5+ years of Go and Kubernetes."}}
	withCtx := checkRequirement{Requirement: domain.Requirement{ID: "req_1", Value: "Go"}, option: "Go", context: "5+ years of Go."}
	noCtx := checkRequirement{Requirement: domain.Requirement{ID: "req_2", Value: "Kafka"}, option: "Kafka"}
	got := map[string]any{
		"section":         sectionQuestion("5+ years of Go and Kubernetes.", "Requirements"),
		"section_no_head": sectionQuestion("Remote, US only.", ""),
		"validation":      validationQuestion(chunk{Text: "5+ years of Go", Options: []string{"Go", "5+ years of Go"}}),
		"filler":          fillerQuestion("Go", m),
		"duplicate":       duplicateQuestion("Kubernetes", m, []string{"K8s", "Go"}),
		"alternative":     alternativeQuestion("Go", m, []string{"Rust"}),
		"importance":      importanceQuestion("Go", m),
		"summary_system":  summarySystemPrompt,
		"retrieval":       retrievalQuestion([]checkRequirement{withCtx, noCtx}),
		"gate":            gateQuestion(withCtx),
		"gate_no_context": gateQuestion(noCtx),
		"strength":        strengthQuestion(withCtx, strengthCriteria),
		"strength_no_ctx": strengthQuestion(noCtx, strengthCriteria),
		"holistic":        holisticQuestions,
	}
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "prompts.golden.json")
	if *updateSnapshot {
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if string(append(raw, '\n')) != string(want) {
		t.Errorf("prompts changed; review with: go test ./internal/app -run TestPromptSnapshot -update && git diff %s", path)
	}
}
