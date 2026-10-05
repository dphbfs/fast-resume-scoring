package app

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// sectionState is the Jev state for Section labeling: the posting as
// background. Each question carries its own sentence and heading.
type sectionState struct {
	JobTitle   string `json:"job_title"`
	JobPosting string `json:"job_posting"`
}

// labelSections assigns a Section to every Context Sentence. Questions are
// split into batches of cfg.SectionBatchSize; every batch sends the same
// state and batches run concurrently (bounded by the Jev client's limiter).
func (e *Extractor) labelSections(ctx context.Context, r *run) error {
	state := sectionState{JobTitle: r.jd.Title, JobPosting: r.jd.Text}

	valid := make(map[string]bool, len(domain.Sections))
	for _, s := range domain.Sections {
		valid[string(s)] = true
	}

	r.sectionConf = make([]float64, len(r.sentences))
	batch := max(1, e.cfg.SectionBatchSize)
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	for lo := 0; lo < len(r.sentences); lo += batch {
		hi := min(lo+batch, len(r.sentences))
		g.Go(func() error {
			questions := make(map[string]port.Question, hi-lo)
			for i := lo; i < hi; i++ {
				questions[sectionQuestionID(i)] = sectionQuestion(r.sentences[i].Text, r.headings[i])
			}
			resp, err := e.classifier.Classify(ctx, port.ClassifyRequest{State: state, Questions: questions})
			if err != nil {
				return err
			}
			e.addUsage("extract.sections", resp.Usage)

			mu.Lock()
			defer mu.Unlock()
			r.model = resp.Model
			for i := lo; i < hi; i++ {
				a := resp.Answers[sectionQuestionID(i)]
				if !valid[a.Choice] {
					return fmt.Errorf("sentence %s: unknown section %q", r.sentences[i].Ref, a.Choice)
				}
				r.sentences[i].Section = domain.Section(a.Choice)
				if a.Confidence != nil {
					r.sectionConf[i] = *a.Confidence
				}
				if a.Confidence != nil && *a.Confidence < lowSectionConfidence {
					e.metrics.Add("sections.low_confidence", 1)
					e.log.DebugContext(ctx, "uncertain section",
						"ref", r.sentences[i].Ref, "section", a.Choice,
						"confidence", *a.Confidence, "text", r.sentences[i].Text)
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	for _, s := range r.sentences {
		e.metrics.Add("sections."+string(s.Section), 1)
		if droppedSections[s.Section] {
			e.metrics.Add("sections.dropped", 1)
			e.log.DebugContext(ctx, "sentence dropped", "ref", s.Ref, "section", s.Section, "text", s.Text)
		}
	}
	return nil
}
