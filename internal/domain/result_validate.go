package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Validate checks a Result read from outside the process (a schema v1 file)
// before it is used: unique non-empty Requirement IDs and values, known
// Tiers, Refs that exist in Context, Importance in [0, 1], and Alternative
// Groups of at least two known Requirements, each Requirement in at most
// one group. It reports every problem found.
func (r Result) Validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	for ref, s := range r.Context {
		if !knownSection(s.Section) {
			bad("context %s: unknown section %q", ref, s.Section)
		}
	}
	ids := make(map[string]bool, len(r.Requirements))
	for i, q := range r.Requirements {
		switch {
		case q.ID == "":
			bad("requirement %d: empty id", i)
		case ids[q.ID]:
			bad("requirement %s: duplicate id", q.ID)
		}
		ids[q.ID] = true
		if strings.TrimSpace(q.Value) == "" {
			bad("requirement %s: empty value", q.ID)
		}
		switch q.Tier {
		case TierRequired, TierPreferred, TierMentioned:
		default:
			bad("requirement %s: unknown tier %q", q.ID, q.Tier)
		}
		if math.IsNaN(q.Importance) || q.Importance < 0 || q.Importance > 1 {
			bad("requirement %s: importance %v outside [0, 1]", q.ID, q.Importance)
		}
		if len(q.Refs) == 0 {
			bad("requirement %s: no refs", q.ID)
		}
		for _, ref := range q.Refs {
			if _, ok := r.Context[ref]; !ok {
				bad("requirement %s: ref %s not in context", q.ID, ref)
			}
		}
	}
	groupOf := map[string]string{}
	groupIDs := map[string]bool{}
	for i, g := range r.AlternativeGroups {
		switch {
		case g.ID == "":
			bad("alternative group %d: empty id", i)
		case groupIDs[g.ID]:
			bad("alternative group %s: duplicate id", g.ID)
		}
		groupIDs[g.ID] = true
		if len(g.Members) < 2 {
			bad("alternative group %s: %d members, want at least 2", g.ID, len(g.Members))
		}
		for _, m := range g.Members {
			if !ids[m] {
				bad("alternative group %s: unknown member %s", g.ID, m)
			}
			if other, ok := groupOf[m]; ok {
				bad("requirement %s: in groups %s and %s", m, other, g.ID)
			}
			groupOf[m] = g.ID
		}
	}
	return errors.Join(errs...)
}

func knownSection(s Section) bool {
	switch s {
	case SectionRequired, SectionPreferred, SectionResponsibilities, SectionCompany, SectionBenefits, SectionOther:
		return true
	}
	return false
}
