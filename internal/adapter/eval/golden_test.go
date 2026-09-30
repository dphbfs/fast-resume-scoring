package eval

import (
	"slices"
	"strings"
	"testing"
)

// TestGoldenLabelsAreConsistent lints every golden label file against the
// rules in testdata/golden/README.md that a machine can check.
func TestGoldenLabelsAreConsistent(t *testing.T) {
	fixtures, err := LoadGolden("../../../testdata/golden", nil)
	if err != nil {
		t.Fatal(err)
	}
	tiers := []string{"required", "preferred", "mentioned"}
	for _, f := range fixtures {
		exp := f.Expected
		values := map[string]bool{}
		for _, r := range exp.Requirements {
			v := normalize(r.Value)
			if values[v] {
				t.Errorf("%s: duplicate value %q", f.ID, r.Value)
			}
			values[v] = true
			if !slices.Contains(tiers, r.Tier) {
				t.Errorf("%s: %q has tier %q", f.ID, r.Value, r.Tier)
			}
		}
		for _, r := range exp.Requirements {
			for _, a := range r.Aliases {
				if n := normalize(a); n != normalize(r.Value) && values[n] {
					t.Errorf("%s: alias %q of %q is another requirement's value", f.ID, a, r.Value)
				}
			}
		}
		seen := map[string]bool{}
		for _, g := range exp.AlternativeGroups {
			if len(g) < 2 {
				t.Errorf("%s: group %q has fewer than 2 members", f.ID, g)
			}
			for _, m := range g {
				if !values[normalize(m)] {
					t.Errorf("%s: group member %q is not a requirement", f.ID, m)
				}
				if seen[normalize(m)] {
					t.Errorf("%s: %q is in two groups", f.ID, m)
				}
				seen[normalize(m)] = true
			}
		}
		for _, list := range []struct {
			name    string
			phrases []string
		}{{"acceptable", exp.Acceptable}, {"filler", exp.Filler}} {
			for _, p := range list.phrases {
				if values[normalize(p)] {
					t.Errorf("%s: %s phrase %q is also a requirement", f.ID, list.name, p)
				}
				if strings.TrimSpace(p) == "" {
					t.Errorf("%s: empty %s phrase", f.ID, list.name)
				}
			}
		}
	}
}
