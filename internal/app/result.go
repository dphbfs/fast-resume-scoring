package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

// buildResult assembles the schema v1 Result: validated Requirements merged
// by case-insensitive value (keeping the first spelling and every Ref),
// Importance and Alternative Groups from the Refinement Round, and the
// Context Sentences that any Requirement refers to.
func (e *Extractor) buildResult(_ context.Context, r *run) error {
	type entry struct {
		value string
		refs  []domain.Ref
		order int
	}
	byKey := map[string]*entry{}
	var keys []string
	for _, a := range r.accepted {
		k := strings.ToLower(a.Text)
		en, ok := byKey[k]
		if !ok {
			en = &entry{value: a.Text, order: len(keys)}
			byKey[k] = en
			keys = append(keys, k)
		}
		if !slices.Contains(en.refs, a.Ref) {
			en.refs = append(en.refs, a.Ref)
		}
	}
	slices.SortStableFunc(keys, func(a, b string) int {
		ia, ib := r.importance[a], r.importance[b]
		switch {
		case ia > ib:
			return -1
		case ia < ib:
			return 1
		}
		return byKey[a].order - byKey[b].order
	})

	sentences := make(map[domain.Ref]domain.ContextSentence, len(r.sentences))
	for _, s := range r.sentences {
		sentences[s.Ref] = s
	}

	res := domain.Result{
		SchemaVersion:     domain.SchemaVersion,
		Model:             r.model,
		Requirements:      make([]domain.Requirement, 0, len(keys)),
		AlternativeGroups: []domain.AlternativeGroup{},
		Context:           map[domain.Ref]domain.ContextSentence{},
	}
	ids := map[string]string{}
	for i, k := range keys {
		en := byKey[k]
		id := fmt.Sprintf("req_%d", i+1)
		ids[k] = id
		res.Requirements = append(res.Requirements, domain.Requirement{
			ID: id, Value: en.value, Refs: en.refs, Importance: r.importance[k],
		})
		for _, ref := range en.refs {
			res.Context[ref] = sentences[ref]
		}
	}
	for i, g := range r.groups {
		var members []string
		for _, v := range g {
			if id, ok := ids[strings.ToLower(v)]; ok {
				members = append(members, id)
			}
		}
		if len(members) > 1 {
			res.AlternativeGroups = append(res.AlternativeGroups,
				domain.AlternativeGroup{ID: fmt.Sprintf("alt_%d", i+1), Members: members})
		}
	}

	e.metrics.Add("requirements.total", int64(len(res.Requirements)))
	r.result = res
	return nil
}
