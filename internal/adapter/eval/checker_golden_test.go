package eval

import (
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

// TestCheckerLabelsAreConsistent lints the Resume Checker labels: keys name
// golden Requirements, quotes resolve to exactly one Evidence Unit (at most
// once per Requirement), strengths are valid, and Skills or Summary units
// are never labeled above weak.
func TestCheckerLabelsAreConsistent(t *testing.T) {
	fixtures, err := LoadCheckerGolden("../../../testdata/checker", "../../../testdata/golden", "../../../testdata/resumes", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		values := map[string]bool{}
		for _, r := range f.Job.Expected.Requirements {
			values[r.Value] = true
		}
		units := map[string]domain.EvidenceUnit{}
		for _, u := range f.Units {
			units[u.ID] = u
		}
		for _, s := range f.Expected.Skip {
			if !values[s] {
				t.Errorf("%s: skip %q is not a requirement", f.ID, s)
			}
		}
		t.Logf("%s: %d units, %d linked requirements", f.ID, len(f.Units), len(f.Expected.Links))
		for req, labels := range f.Expected.Links {
			if !values[req] {
				t.Errorf("%s: %q is not a requirement", f.ID, req)
			}
			seen := map[string]bool{}
			for _, l := range labels {
				id, err := ResolveQuote(f.Units, l.Quote)
				if err != nil {
					t.Errorf("%s: %q: %v", f.ID, req, err)
					continue
				}
				if seen[id] {
					t.Errorf("%s: %q links unit %s twice", f.ID, req, id)
				}
				seen[id] = true
				if strengthRank[l.Strength] == 0 {
					t.Errorf("%s: %q has strength %q", f.ID, req, l.Strength)
				}
				if sec := units[id].ResumeSection; (sec == domain.ResumeSkills || sec == domain.ResumeSummary) && l.Strength != StrengthWeak {
					t.Errorf("%s: %q: %s unit %q labeled %s, want weak", f.ID, req, sec, l.Quote, l.Strength)
				}
			}
		}
	}
}
