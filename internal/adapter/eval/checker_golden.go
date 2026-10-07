package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dphbfs/fast-resume-scoring/internal/app"
	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

// Evidence Strength labels.
const (
	StrengthStrong  = "strong"
	StrengthPartial = "partial"
	StrengthWeak    = "weak"
)

// strengthRank orders Evidence Strengths; 0 is none.
var strengthRank = map[string]int{StrengthWeak: 1, StrengthPartial: 2, StrengthStrong: 3}

// EvidenceLabel is one labeled Evidence Link: a quote that identifies one
// Evidence Unit of the Resume, and its Evidence Strength.
type EvidenceLabel struct {
	Quote    string `json:"quote"`
	Strength string `json:"strength"`
}

// CheckerExpected is the content of testdata/checker/<pair>.expected.json.
// Requirements missing from Links have Coverage none; Skip lists
// Requirements left out of scoring (years qualifiers).
type CheckerExpected struct {
	Job    string                     `json:"job"`
	Resume string                     `json:"resume"`
	Skip   []string                   `json:"skip"`
	Links  map[string][]EvidenceLabel `json:"links"`
	Notes  string                     `json:"notes"`
}

// CheckerFixture is one (Job Description, Resume) pair with its labels.
type CheckerFixture struct {
	ID       string
	Job      Fixture
	Resume   domain.Resume
	Units    []domain.EvidenceUnit
	Expected CheckerExpected
}

// LoadCheckerGolden reads every <pair>.expected.json in dir, with the golden
// Job Description it names (from goldenDir) and its Resume (from
// resumesDir). Fixtures whose ID does not start with an entry of only are
// skipped when only is non-empty.
func LoadCheckerGolden(dir, goldenDir, resumesDir string, only []string) ([]CheckerFixture, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.expected.json"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	jobs, err := LoadGolden(goldenDir, nil)
	if err != nil {
		return nil, err
	}
	var out []CheckerFixture
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".expected.json")
		if len(only) > 0 && !slices.ContainsFunc(only, func(p string) bool { return strings.HasPrefix(id, p) }) {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var exp CheckerExpected
		if err := json.Unmarshal(raw, &exp); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		i := slices.IndexFunc(jobs, func(j Fixture) bool { return j.ID == exp.Job })
		if i < 0 {
			return nil, fmt.Errorf("%s: unknown job %q", f, exp.Job)
		}
		text, err := os.ReadFile(filepath.Join(resumesDir, exp.Resume+".md"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		resume := domain.Resume{Text: string(text)}
		out = append(out, CheckerFixture{
			ID: id, Job: jobs[i], Resume: resume, Units: app.ParseResume(resume.Text), Expected: exp,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no checker fixtures in %s", dir)
	}
	return out, nil
}

// ResolveQuote returns the ID of the one Evidence Unit whose text contains
// quote, or an error when none or several do.
func ResolveQuote(units []domain.EvidenceUnit, quote string) (string, error) {
	var ids []string
	for _, u := range units {
		if strings.Contains(u.Text, quote) {
			ids = append(ids, u.ID)
		}
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("quote %q matches %d units %v, want 1", quote, len(ids), ids)
	}
	return ids[0], nil
}
