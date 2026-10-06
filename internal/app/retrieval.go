package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// traceTopRetrieval is how many options a TraceRound keeps.
const traceTopRetrieval = 8

// retrieve proposes the Requirements an Evidence Unit might support: one
// Choice over every Requirement plus the none sink, repeated so that each
// round offers only the best NarrowSizes[i] Requirements of the previous
// round (probability the dropped options held is redistributed among the
// survivors). The last round keeps its top RetrievalK at or above
// RetrievalFloor. With no NarrowSizes it is a single round.
func (c *Checker) retrieve(ctx context.Context, u domain.EvidenceUnit, creqs []checkRequirement) (retrieved []int, rounds []domain.TraceRound, model string, err error) {
	all := make([]int, len(creqs))
	for i := range all {
		all[i] = i
	}
	ask := func(offered []int) ([]domain.TraceOption, error) {
		ranked, m, err := c.askRetrieval(ctx, u, creqs, offered)
		if err != nil {
			return nil, err
		}
		model = m
		rounds = append(rounds, domain.TraceRound{Options: len(offered) + 1, Top: ranked[:min(len(ranked), traceTopRetrieval)], Kept: []string{}})
		return ranked, nil
	}
	// keep records the kept Requirements on the last round's trace.
	keep := func(ids []int) {
		for _, i := range ids {
			rounds[len(rounds)-1].Kept = append(rounds[len(rounds)-1].Kept, creqs[i].Value)
		}
	}
	byOption := make(map[string]int, len(creqs))
	for i, r := range creqs {
		byOption[r.option] = i
	}
	// best returns up to n Requirements from ranked, highest first, with
	// probability at or above floor.
	best := func(ranked []domain.TraceOption, n int, floor float64) []int {
		var out []int
		for _, o := range ranked {
			if i, ok := byOption[o.Option]; ok && o.P >= floor && len(out) < n {
				out = append(out, i)
			}
		}
		return out
	}

	offered := all
	sizes := append(slices.Clone(c.cfg.NarrowSizes), 0) // 0: final round
	for _, size := range sizes {
		ranked, err := ask(offered)
		if err != nil {
			return nil, rounds, "", err
		}
		if size == 0 {
			retrieved = best(ranked, c.retrievalK(), c.retrievalFloor())
			keep(retrieved)
			break
		}
		next := best(ranked, size, 0)
		keep(next)
		if len(next) <= 1 {
			// Nothing left to compare; keep it if it cleared the floor.
			retrieved = best(ranked, 1, c.retrievalFloor())
			break
		}
		offered = next
	}

	c.metrics.Add("checker.retrieval.pairs", int64(len(retrieved)))
	if len(retrieved) == 0 {
		c.metrics.Add("checker.retrieval.empty_units", 1)
	}
	return retrieved, rounds, model, nil
}

// askRetrieval asks one Retrieval Choice over the offered Requirements plus
// none and returns every option ranked by probability.
func (c *Checker) askRetrieval(ctx context.Context, u domain.EvidenceUnit, creqs []checkRequirement, offered []int) ([]domain.TraceOption, string, error) {
	opts := make([]checkRequirement, len(offered))
	for k, i := range offered {
		opts[k] = creqs[i]
	}
	resp, err := c.classifier.Classify(ctx, port.ClassifyRequest{
		State:     stateOf(u),
		Questions: map[string]port.Question{"retrieval": c.prompts.retrievalQuestion(opts)},
	})
	if err != nil {
		return nil, "", err
	}
	c.metrics.Add("checker.retrieval.requests", 1)
	c.addUsage("checker.retrieval", resp.Usage)
	a := resp.Answers["retrieval"]
	if a.Choice != noneOption && !slices.ContainsFunc(opts, func(r checkRequirement) bool { return r.option == a.Choice }) {
		return nil, "", fmt.Errorf("answer %q is not an option", a.Choice)
	}
	return topOptions(a.Probabilities, len(a.Probabilities)), resp.Model, nil
}

// retrievalQuestion offers the given Requirements, each described by its
// Context Sentence, plus the none sink.
func (p *prompts) retrievalQuestion(creqs []checkRequirement) port.Question {
	criteria := make(map[string]any, len(creqs)+1)
	for _, r := range creqs {
		if r.context != "" {
			criteria[r.option] = p.retrieval.ContextPrefix + r.context
		} else {
			criteria[r.option] = nil
		}
	}
	criteria[noneOption] = p.retrieval.None
	return port.Question{
		Type:         port.Choice,
		Instructions: p.retrieval.Question,
		Criteria:     criteria,
	}
}
