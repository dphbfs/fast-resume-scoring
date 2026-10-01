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

// retrieve proposes the Requirements an Evidence Unit might support, by
// RetrievalMode:
//
//   - single: one Choice over every Requirement; keep the top RetrievalK at
//     or above RetrievalFloor.
//   - narrow: the same, repeated: each round offers only the best
//     NarrowSizes[i] Requirements of the previous round, so probability
//     the dropped options held is redistributed among the survivors. The
//     last round keeps its top RetrievalK at or above the floor.
//   - noul: one yes/no question per Requirement, in one request; keep the
//     top RetrievalK with P(yes) >= NoulThreshold. Probabilities are
//     independent, so a unit that supports many Requirements does not
//     split its mass among them.
//   - peel: take the most probable Requirement, remove it, and ask again,
//     until none wins or RetrievalK are taken. A Choice concentrates on one
//     option, so peeling lets the 2nd..Kth Requirement win a round of its
//     own. PeelShortlist > 0 first narrows to that many with one Choice.
//
// "none" is a sink in single and narrow mode; in peel mode it ends the
// loop when it wins.
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

	switch c.retrievalMode() {
	case "noul":
		ranked, err := c.askNoul(ctx, u, creqs)
		if err != nil {
			return nil, rounds, "", err
		}
		model = ranked.model
		rounds = append(rounds, domain.TraceRound{Options: len(creqs), Top: ranked.top[:min(len(ranked.top), traceTopRetrieval)], Kept: []string{}})
		retrieved = best(ranked.top, c.retrievalK(), c.noulThreshold())
		keep(retrieved)
	case "narrow":
		offered := all
		sizes := append(slices.Clone(c.cfg.NarrowSizes), 0) // 0: final round
		for r, size := range sizes {
			ranked, err := ask(offered)
			if err != nil {
				return nil, rounds, "", err
			}
			if size == 0 || r == len(sizes)-1 {
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
	case "peel":
		remaining := all
		if n := c.cfg.PeelShortlist; n > 0 && n < len(all) {
			ranked, err := ask(all)
			if err != nil {
				return nil, rounds, "", err
			}
			remaining = best(ranked, n, 0)
			keep(remaining)
		}
		for len(retrieved) < c.retrievalK() && len(remaining) > 0 {
			ranked, err := ask(remaining)
			if err != nil {
				return nil, rounds, "", err
			}
			win, ok := byOption[ranked[0].Option]
			if !ok {
				break // none won
			}
			retrieved = append(retrieved, win)
			keep([]int{win})
			remaining = slices.DeleteFunc(slices.Clone(remaining), func(i int) bool { return i == win })
		}
	default:
		ranked, err := ask(all)
		if err != nil {
			return nil, rounds, "", err
		}
		retrieved = best(ranked, c.retrievalK(), c.retrievalFloor())
		keep(retrieved)
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
		Questions: map[string]port.Question{"retrieval": retrievalQuestion(opts)},
	})
	if err != nil {
		return nil, "", err
	}
	c.metrics.Add("checker.retrieval.requests", 1)
	a := resp.Answers["retrieval"]
	if a.Choice != noneOption && !slices.ContainsFunc(opts, func(r checkRequirement) bool { return r.option == a.Choice }) {
		return nil, "", fmt.Errorf("answer %q is not an option", a.Choice)
	}
	return topOptions(a.Probabilities, len(a.Probabilities)), resp.Model, nil
}

// retrievalQuestion offers the given Requirements, each described by its
// Context Sentence, plus the none sink.
func retrievalQuestion(creqs []checkRequirement) port.Question {
	criteria := make(map[string]any, len(creqs)+1)
	for _, r := range creqs {
		if r.context != "" {
			criteria[r.option] = "Job posting: " + r.context
		} else {
			criteria[r.option] = nil
		}
	}
	criteria[noneOption] = "The statement is not evidence for any of the listed job requirements."
	return port.Question{
		Type: port.Choice,
		Instructions: "A resume `statement` is checked against a job's requirements. " +
			"Which requirement does this statement most directly give evidence for? " +
			"Choose none when it demonstrates none of them.",
		Criteria: criteria,
	}
}

// noulRanking is every Requirement ranked by P(yes) of its retrieval Noul.
type noulRanking struct {
	top   []domain.TraceOption // Option is the Requirement's option key
	model string
}

// askNoul asks, in one request, one Noul per Requirement: could the unit be
// evidence for it?
func (c *Checker) askNoul(ctx context.Context, u domain.EvidenceUnit, creqs []checkRequirement) (noulRanking, error) {
	questions := make(map[string]port.Question, len(creqs))
	for i, r := range creqs {
		inst := map[string]any{
			"task":        "Could this resume `statement` serve as evidence, even partial, for the job requirement?",
			"requirement": r.Value,
		}
		if r.context != "" {
			inst["job_posting_context"] = r.context
		}
		questions[fmt.Sprintf("req_%d", i)] = port.Question{
			Type:         port.Noul,
			Instructions: inst,
			Criteria: map[string]any{
				"true": "The statement shows, names, or clearly involves work with the requirement or a specific " +
					"instance of it, even partly or only as a listed skill.",
				"false": "The statement is about something else: it only shares words with the requirement, uses a " +
					"competing tool, or shows unrelated skills.",
			},
		}
	}
	resp, err := c.classifier.Classify(ctx, port.ClassifyRequest{State: stateOf(u), Questions: questions})
	if err != nil {
		return noulRanking{}, err
	}
	c.metrics.Add("checker.retrieval.requests", 1)
	probs := make(map[string]float64, len(creqs))
	for i, r := range creqs {
		a, ok := resp.Answers[fmt.Sprintf("req_%d", i)]
		if !ok || a.Noul == nil {
			return noulRanking{}, fmt.Errorf("requirement %q: no noul answer", r.Value)
		}
		probs[r.option] = *a.Noul
	}
	return noulRanking{top: topOptions(probs, len(probs)), model: resp.Model}, nil
}
