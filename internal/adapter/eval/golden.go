// Package eval is a driving adapter that runs the Requirement Extractor on
// the golden set and scores the results against the expected labels.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/cli"
	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

// ExpectedRequirement is one labeled Requirement (testdata/golden/README.md).
type ExpectedRequirement struct {
	Value   string   `json:"value"`
	Aliases []string `json:"aliases"`
	Tier    string   `json:"tier"`
}

// Expected is the content of <id>.expected.json.
type Expected struct {
	Requirements      []ExpectedRequirement `json:"requirements"`
	AlternativeGroups [][]string            `json:"alternative_groups"`
	Filler            []string              `json:"filler"`
	Notes             string                `json:"notes"`
}

// Fixture is one golden Job Description with its labels.
type Fixture struct {
	ID       string
	JD       domain.JobDescription
	Expected Expected
}

// LoadGolden reads every <id>.expected.json in dir with its <id>.txt. If only
// is non-empty, fixtures whose ID does not start with one of its entries are
// skipped.
func LoadGolden(dir string, only []string) ([]Fixture, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.expected.json"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	var out []Fixture
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".expected.json")
		if len(only) > 0 && !slices.ContainsFunc(only, func(p string) bool { return strings.HasPrefix(id, p) }) {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var exp Expected
		if err := json.Unmarshal(raw, &exp); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		jd, err := cli.ReadJobDescription(filepath.Join(dir, id+".txt"))
		if err != nil {
			return nil, err
		}
		out = append(out, Fixture{ID: id, JD: jd, Expected: exp})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no golden fixtures in %s", dir)
	}
	return out, nil
}
