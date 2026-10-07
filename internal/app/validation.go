package app

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
)

// judged is the Candidate Jev selected for a chunk, with the probability of
// that selection.
type judged struct {
	domain.Candidate
	P float64
}

// validationState is the Jev state for one sentence's chunks.
type validationState struct {
	JobSummary string         `json:"job_summary"`
	Section    domain.Section `json:"section"`
	Sentence   string         `json:"sentence"`
}

// validationRound asks, for every chunk, which of its options names the
// Requirement (or none), with one Jev request per sentence (split at
// validationBatchSize). Chunks do not overlap, so selections cannot contain
// one another.
func (e *Extractor) validationRound(ctx context.Context, r *run) error {
	sentences := make(map[domain.Ref]domain.ContextSentence, len(r.sentences))
	for _, s := range r.sentences {
		sentences[s.Ref] = s
	}

	// Batch chunks by sentence, preserving order. Each batch writes only its
	// own results, so no locking is needed.
	type batch struct {
		chunks   []chunk
		out      []judged
		rejected map[string]int
		trace    []domain.TraceChunk
		splits   int
	}
	var batches []*batch
	for i := 0; i < len(r.chunks); {
		j := i
		for j < len(r.chunks) && r.chunks[j].Ref == r.chunks[i].Ref && j-i < validationBatchSize {
			j++
		}
		batches = append(batches, &batch{chunks: r.chunks[i:j], rejected: map[string]int{}})
		i = j
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, b := range batches {
		g.Go(func() error {
			s := sentences[b.chunks[0].Ref]
			questions := make(map[string]port.Question, len(b.chunks))
			for i, c := range b.chunks {
				questions[fmt.Sprintf("chunk_%d", i)] = e.prompts.validationQuestion(c)
			}
			resp, err := e.classifier.Classify(gctx, port.ClassifyRequest{
				State:     validationState{JobSummary: r.summary, Section: s.Section, Sentence: s.Text},
				Questions: questions,
			})
			if err != nil {
				return fmt.Errorf("sentence %s: %w", s.Ref, err)
			}
			e.addUsage("extract.validation", resp.Usage)
			for i, c := range b.chunks {
				a := resp.Answers[fmt.Sprintf("chunk_%d", i)]
				if _, ok := a.Probabilities[a.Choice]; !ok || !isOption(c, a.Choice, e.prompts.rejectOptions) {
					return fmt.Errorf("sentence %s: chunk %q: answer %q is not an option", s.Ref, c.Text, a.Choice)
				}
				best, bestP, spanMass := decide(c, a.Probabilities)
				tc := domain.TraceChunk{
					Ref: c.Ref, Text: c.Text, Options: len(c.Options), RequirementMass: spanMass,
					Top: topOptions(a.Probabilities, traceTopOptions), Candidates: c.Options,
				}
				if spanMass >= e.minRequirementMass() {
					parts := splitSlashSelection(best)
					if len(parts) > 1 {
						b.splits++
					}
					for _, p := range parts {
						b.out = append(b.out, judged{Candidate: domain.Candidate{Text: p, Ref: c.Ref}, P: bestP})
					}
					tc.Selected, tc.SelectedP = best, bestP
				} else {
					reason := topReject(a.Probabilities, e.prompts.rejectOptions)
					b.rejected[reason]++
					tc.RejectReason = reason
				}
				b.trace = append(b.trace, tc)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	r.accepted = r.accepted[:0]
	r.trace.Chunks = nil
	for _, b := range batches {
		r.accepted = append(r.accepted, b.out...)
		r.trace.Chunks = append(r.trace.Chunks, b.trace...)
		e.metrics.Add("validation.slash_split", int64(b.splits))
		for reason, n := range b.rejected {
			e.metrics.Add("validation.rejected."+reason, int64(n))
		}
	}
	e.metrics.Add("validation.requests", int64(len(batches)))
	e.metrics.Add("validation.accepted", int64(len(r.accepted)))
	return nil
}

// decide sums the probability on the chunk's Candidate options and returns
// the most probable Candidate. Grouping matters: overlapping Candidates split
// "yes" between them, and the rejectOptions split "no".
func decide(c chunk, probs map[string]float64) (best string, bestP, spanMass float64) {
	for _, o := range c.Options {
		p := probs[o]
		spanMass += p
		if p > bestP {
			best, bestP = o, p
		}
	}
	return best, bestP, spanMass
}

// splitSlashSelection splits a selected Candidate that is a single
// slash-joined token ("terraform/terragrunt", "OpenSSL/AWS-LC") into its
// parts, which name separate Requirements. It keeps short pairs ("CI/CD")
// and multi-word selections ("client/server architectures") whole.
func splitSlashSelection(sel string) []string {
	if strings.ContainsAny(sel, " \t") || !strings.Contains(sel, "/") {
		return []string{sel}
	}
	parts := strings.Split(sel, "/")
	for _, p := range parts {
		if len([]rune(p)) < 3 || !hasLetter(p) {
			return []string{sel}
		}
	}
	return parts
}

// traceTopOptions is how many options a TraceChunk keeps.
const traceTopOptions = 5

// topOptions returns the n most probable options, highest first.
func topOptions(probs map[string]float64, n int) []domain.TraceOption {
	out := make([]domain.TraceOption, 0, len(probs))
	for o, p := range probs {
		out = append(out, domain.TraceOption{Option: o, P: p})
	}
	slices.SortFunc(out, func(a, b domain.TraceOption) int {
		if a.P != b.P {
			if a.P > b.P {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Option, b.Option)
	})
	return out[:min(n, len(out))]
}

// topReject returns the most probable rejection reason.
func topReject(probs map[string]float64, rejectOptions map[string]string) string {
	best, bestP := "", -1.0
	for _, k := range slices.Sorted(maps.Keys(rejectOptions)) {
		if probs[k] > bestP {
			best, bestP = k, probs[k]
		}
	}
	return best
}

func isOption(c chunk, o string, rejectOptions map[string]string) bool {
	_, reject := rejectOptions[o]
	return reject || slices.Contains(c.Options, o)
}

// minRequirementMass is cfg.MinRequirementMass, defaulting to 0.5 when unset
// (as in tests that build a zero Pipeline config).
func (e *Extractor) minRequirementMass() float64 {
	if e.cfg.MinRequirementMass > 0 {
		return e.cfg.MinRequirementMass
	}
	return 0.5
}
