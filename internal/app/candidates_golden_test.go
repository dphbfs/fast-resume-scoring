package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minGoldenCoverage is the share of golden Requirements (by value or alias)
// that must appear verbatim among Candidates. Jev can only accept a phrase it
// is shown, so this caps extraction recall. Misses today are mostly label
// paraphrases that eval matches fuzzily.
const minGoldenCoverage = 0.93

func TestCandidateCoverageOfGoldenSet(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/golden/*.expected.json")
	if len(files) == 0 {
		t.Skip("no golden fixtures")
	}
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "’", "'")) }

	covered, total := 0, 0
	var missed []string
	for _, f := range files {
		var exp struct {
			Requirements []struct {
				Value   string   `json:"value"`
				Aliases []string `json:"aliases"`
			} `json:"requirements"`
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &exp); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		text, err := os.ReadFile(strings.TrimSuffix(f, ".expected.json") + ".txt")
		if err != nil {
			t.Fatal(err)
		}

		sentences, _ := splitSentences(string(text))
		candidates := map[string]bool{}
		for _, s := range sentences {
			for _, c := range sentenceCandidates(s, defaultMaxWindowWords) {
				candidates[norm(c.Text)] = true
			}
		}
		for _, r := range exp.Requirements {
			total++
			ok := candidates[norm(r.Value)]
			for _, a := range r.Aliases {
				ok = ok || candidates[norm(a)]
			}
			if ok {
				covered++
			} else {
				missed = append(missed, r.Value)
			}
		}
	}

	got := float64(covered) / float64(total)
	t.Logf("golden coverage %d/%d = %.1f%%", covered, total, 100*got)
	if got < minGoldenCoverage {
		t.Errorf("coverage %.1f%% < %.0f%%; missed: %q", 100*got, 100*minGoldenCoverage, missed)
	}
}
