package app

import (
	"strings"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

// ExtractorVersion changes whenever the same Job Description text can
// produce a different Result for reasons outside the configuration (input
// handling, questions). Caches of extraction results key on it.
const ExtractorVersion = "2"

// fullPostingMarker starts the original posting in Job Descriptions saved
// by the job-search agent, after its own condensed "Stack &
// Responsibilities" summary of the same posting.
const fullPostingMarker = "\n### Full Job Description\n"

// postingText drops a derived summary that precedes the original posting:
// when the text has a full-posting section, only its first line (the title
// heading) and that section are kept, so each Requirement is read once.
// Other text is returned unchanged.
func postingText(jd domain.JobDescription) domain.JobDescription {
	i := strings.Index(jd.Text, fullPostingMarker)
	if i < 0 {
		return jd
	}
	title, _, _ := strings.Cut(jd.Text, "\n")
	jd.Text = title + "\n\n" + strings.TrimLeft(jd.Text[i+len(fullPostingMarker):], "\n")
	return jd
}
