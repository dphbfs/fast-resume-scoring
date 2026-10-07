package eval

import (
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

func TestAttributeMiss(t *testing.T) {
	tr := domain.Trace{
		Sentences: []domain.TraceSentence{
			{Ref: "s1", Text: "Dental insurance and Kafka perks", Section: domain.SectionBenefits, Dropped: true},
			{Ref: "s2", Text: "Experience with streaming data such as Flink", Section: domain.SectionRequired},
			{Ref: "s3", Text: "Rust or Zig", Section: domain.SectionRequired},
		},
		Chunks: []domain.TraceChunk{
			{Ref: "s2", Text: "streaming data", Candidates: []string{"streaming data", "streaming", "data"}, Selected: "data", SelectedP: 0.6},
			{Ref: "s2", Text: "Flink", Candidates: []string{"Flink"}, RejectReason: "generic_trait", RequirementMass: 0.2},
			{Ref: "s3", Text: "Rust", Candidates: []string{"Rust"}, Selected: "Rust"},
		},
		Refinement: []domain.TraceRequirement{
			{Value: "Rust", Kept: false, FillerKind: "vague_term", KeepP: 0.3},
			{Value: "Golang", Kept: true, MergedInto: "Go"},
		},
	}
	tests := []struct {
		req   ExpectedRequirement
		stage string
	}{
		{ExpectedRequirement{Value: "Rust"}, StageFiller},
		{ExpectedRequirement{Value: "Go", Aliases: []string{"Golang"}}, StageMerged},
		{ExpectedRequirement{Value: "streaming data"}, StageNotSelected},
		{ExpectedRequirement{Value: "Flink"}, StageRejected},
		{ExpectedRequirement{Value: "Kafka"}, StageSectionDropped},
		{ExpectedRequirement{Value: "Zig"}, StageNoCandidate},
		{ExpectedRequirement{Value: "COBOL"}, StageNotInText},
	}
	for _, tt := range tests {
		got := attributeMiss(tt.req, tr)
		if got.Stage != tt.stage || got.Detail == "" {
			t.Errorf("attributeMiss(%q) = %+v, want stage %q with detail", tt.req.Value, got, tt.stage)
		}
	}
}
