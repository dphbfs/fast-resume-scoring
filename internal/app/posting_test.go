package app

import (
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

func TestPostingTextKeepsTitleAndFullPosting(t *testing.T) {
	in := "## Acme - Engineer\n\n📍 Type: Remote\n\n### Stack & Responsibilities\n- Go services\n\n### Full Job Description\nWe use Go and Kafka.\n"
	got := postingText(domain.JobDescription{Title: "Engineer", Text: in})
	if want := "## Acme - Engineer\n\nWe use Go and Kafka.\n"; got.Text != want || got.Title != "Engineer" {
		t.Errorf("postingText = %q, want %q", got.Text, want)
	}
	plain := domain.JobDescription{Text: "Engineer\n\nWe use Go.\n"}
	if got := postingText(plain); got != plain {
		t.Errorf("text without a full posting changed: %q", got.Text)
	}
}
