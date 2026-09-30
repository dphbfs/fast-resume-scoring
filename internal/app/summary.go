package app

import (
	"context"
	"errors"
	"strings"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// jobSummary sets r.summary from the generative client, or falls back to the
// title plus the required and preferred sentences when the client is absent,
// unconfigured, failing, or returns nothing.
func (e *Extractor) jobSummary(ctx context.Context, r *run) error {
	if e.generator != nil {
		text, err := e.generator.Generate(ctx, summarySystemPrompt, r.jd.Text)
		text = strings.TrimSpace(text)
		switch {
		case err == nil && text != "":
			r.summary = truncateWords(text, maxSummaryRunes)
			return nil
		case errors.Is(err, port.ErrGenerativeUnavailable):
			e.log.WarnContext(ctx, "job summary: generative client not configured, using fallback")
		case err != nil:
			e.log.WarnContext(ctx, "job summary: generation failed, using fallback", "error", err)
		default:
			e.log.WarnContext(ctx, "job summary: empty reply, using fallback")
		}
	} else {
		e.log.WarnContext(ctx, "job summary: no generative client, using fallback")
	}

	e.metrics.Add("summary.fallback", 1)
	r.summary = fallbackSummary(r)
	return nil
}

// fallbackSummary joins the title with the required and preferred sentences.
func fallbackSummary(r *run) string {
	parts := []string{r.jd.Title}
	for _, s := range r.sentences {
		if s.Section == domain.SectionRequired || s.Section == domain.SectionPreferred {
			parts = append(parts, s.Text)
		}
	}
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !strings.ContainsAny(p[len(p)-1:], ".!?") {
			p += "."
		}
		parts[i] = p
	}
	return truncateWords(strings.Join(parts, " "), maxFallbackRunes)
}

// truncateWords cuts s to at most n runes, at a word boundary when possible.
func truncateWords(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	cut := string(runes[:n])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut)
}
