package app

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// judged is a Candidate with Jev's verdict: P is the probability mass on
// requirementKinds, Kind the most likely option.
type judged struct {
	domain.Candidate
	P    float64
	Kind string
}

// validationState is the Jev state for one sentence's Candidates.
type validationState struct {
	JobSummary string         `json:"job_summary"`
	Section    domain.Section `json:"section"`
	Sentence   string         `json:"sentence"`
}

// validationRound judges every Candidate with one Jev request per sentence
// (split at validationBatchSize), keeps those at or above acceptCandidate,
// then runs the compound check.
func (e *Extractor) validationRound(ctx context.Context, r *run) error {
	// Group Candidates by sentence, preserving order.
	var groups [][]domain.Candidate
	index := map[domain.Ref]int{}
	for _, c := range r.candidates {
		i, ok := index[c.Ref]
		if !ok {
			i = len(groups)
			index[c.Ref] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], c)
	}
	sentences := make(map[domain.Ref]domain.ContextSentence, len(r.sentences))
	for _, s := range r.sentences {
		sentences[s.Ref] = s
	}

	// Each batch writes only its own slots, so no locking is needed.
	type batch struct {
		cands []domain.Candidate
		out   []judged
	}
	var batches []*batch
	for _, g := range groups {
		for lo := 0; lo < len(g); lo += validationBatchSize {
			batches = append(batches, &batch{cands: g[lo:min(lo+validationBatchSize, len(g))]})
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, b := range batches {
		g.Go(func() error {
			s := sentences[b.cands[0].Ref]
			questions := make(map[string]port.Question, len(b.cands))
			for i, c := range b.cands {
				before, after := surroundingWords(s.Text, c.Text, contextWords)
				questions[fmt.Sprintf("candidate_%d", i)] = validationQuestion(c.Text, before, after)
			}
			resp, err := e.classifier.Classify(gctx, port.ClassifyRequest{
				State:     validationState{JobSummary: r.summary, Section: s.Section, Sentence: s.Text},
				Questions: questions,
			})
			if err != nil {
				return fmt.Errorf("sentence %s: %w", s.Ref, err)
			}
			for i, c := range b.cands {
				a := resp.Answers[fmt.Sprintf("candidate_%d", i)]
				if len(a.Probabilities) == 0 {
					return fmt.Errorf("sentence %s: candidate %q: answer has no probabilities", s.Ref, c.Text)
				}
				var p float64
				for _, k := range requirementKinds {
					p += a.Probabilities[k]
				}
				if p >= acceptCandidate {
					b.out = append(b.out, judged{Candidate: c, P: p, Kind: a.Choice})
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	var accepted []judged
	for _, b := range batches {
		accepted = append(accepted, b.out...)
	}
	e.metrics.Add("validation.requests", int64(len(batches)))
	e.metrics.Add("validation.accepted", int64(len(accepted)))
	e.metrics.Add("validation.rejected", int64(len(r.candidates)-len(accepted)))

	r.accepted = e.dropCompounds(ctx, accepted)
	return nil
}

// dropCompounds removes accepted Candidates that contain two or more other
// accepted Candidates from the same sentence without overlap ("Go and
// Kubernetes"). Validation should reject such phrases, so every drop is
// logged as a warning: it means Jev failed the task or its answer was
// misread. Early versions only; see CLAUDE.md.
func (e *Extractor) dropCompounds(ctx context.Context, accepted []judged) []judged {
	kept := accepted[:0:0]
	for _, a := range accepted {
		var spans [][2]int
		for _, b := range accepted {
			if b.Ref != a.Ref || b.Text == a.Text {
				continue
			}
			n := len(strings.Fields(b.Text))
			for _, at := range wordMatches(a.Text, b.Text) {
				spans = append(spans, [2]int{at, at + n})
			}
		}
		if hasDisjointPair(spans) {
			e.metrics.Add("validation.compound_dropped", 1)
			e.log.WarnContext(ctx, "compound candidate accepted by Jev; dropped",
				"ref", a.Ref, "candidate", a.Text, "p", a.P)
			continue
		}
		kept = append(kept, a)
	}
	return kept
}

// contextWords is how many words on each side of a Candidate are shown.
const contextWords = 3

// surroundingWords returns up to n words before and after the first
// whole-word occurrence of candidate in sentence.
func surroundingWords(sentence, candidate string, n int) (before, after string) {
	start := indexWholeWord(sentence, candidate)
	if start < 0 {
		return "", ""
	}
	b := words(sentence[:start])
	a := words(sentence[start+len(candidate):])
	b = b[max(0, len(b)-n):]
	a = a[:min(n, len(a))]
	return strings.Join(b, " "), strings.Join(a, " ")
}

// words splits s on whitespace, dropping punctuation-only pieces.
func words(s string) []string {
	var out []string
	for _, f := range strings.Fields(s) {
		if hasAlnum(f) {
			out = append(out, f)
		}
	}
	return out
}

// indexWholeWord finds sub in s where it is not glued to letters or digits.
func indexWholeWord(s, sub string) int {
	for from := 0; from <= len(s)-len(sub); {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			return -1
		}
		i += from
		end := i + len(sub)
		if (i == 0 || !isWordByte(s[i-1])) && (end == len(s) || !isWordByte(s[end])) {
			return i
		}
		from = i + 1
	}
	return -1
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// wordMatches returns the word indexes in outer where inner's words occur as
// a whole-word, case-insensitive sequence.
func wordMatches(outer, inner string) []int {
	o := strings.Fields(strings.ToLower(outer))
	in := strings.Fields(strings.ToLower(inner))
	var at []int
	for i := 0; i+len(in) <= len(o); i++ {
		match := true
		for j := range in {
			if o[i+j] != in[j] {
				match = false
				break
			}
		}
		if match {
			at = append(at, i)
		}
	}
	return at
}

// hasDisjointPair reports whether any two half-open spans do not overlap.
func hasDisjointPair(spans [][2]int) bool {
	for i := range spans {
		for j := i + 1; j < len(spans); j++ {
			if spans[i][1] <= spans[j][0] || spans[j][1] <= spans[i][0] {
				return true
			}
		}
	}
	return false
}
