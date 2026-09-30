package app

import (
	"context"
	"fmt"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
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
		chunks []chunk
		out    []judged
		none   int
	}
	var batches []*batch
	for i := 0; i < len(r.chunks); {
		j := i
		for j < len(r.chunks) && r.chunks[j].Ref == r.chunks[i].Ref && j-i < validationBatchSize {
			j++
		}
		batches = append(batches, &batch{chunks: r.chunks[i:j]})
		i = j
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, b := range batches {
		g.Go(func() error {
			s := sentences[b.chunks[0].Ref]
			questions := make(map[string]port.Question, len(b.chunks))
			for i, c := range b.chunks {
				questions[fmt.Sprintf("chunk_%d", i)] = validationQuestion(c)
			}
			resp, err := e.classifier.Classify(gctx, port.ClassifyRequest{
				State:     validationState{JobSummary: r.summary, Section: s.Section, Sentence: s.Text},
				Questions: questions,
			})
			if err != nil {
				return fmt.Errorf("sentence %s: %w", s.Ref, err)
			}
			for i, c := range b.chunks {
				a := resp.Answers[fmt.Sprintf("chunk_%d", i)]
				switch {
				case a.Choice == noRequirement:
					b.none++
				case slices.Contains(c.Options, a.Choice):
					b.out = append(b.out, judged{
						Candidate: domain.Candidate{Text: a.Choice, Ref: c.Ref},
						P:         a.Probabilities[a.Choice],
					})
				default:
					return fmt.Errorf("sentence %s: chunk %q: answer %q is not an option", s.Ref, c.Text, a.Choice)
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	r.accepted = r.accepted[:0]
	none := 0
	for _, b := range batches {
		r.accepted = append(r.accepted, b.out...)
		none += b.none
	}
	e.metrics.Add("validation.requests", int64(len(batches)))
	e.metrics.Add("validation.accepted", int64(len(r.accepted)))
	e.metrics.Add("validation.no_requirement", int64(none))
	return nil
}
