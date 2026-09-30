package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

type fakeGenerator struct {
	reply  string
	err    error
	prompt string
}

func (f *fakeGenerator) Generate(_ context.Context, _, prompt string) (string, error) {
	f.prompt = prompt
	return f.reply, f.err
}

func summaryRun() *run {
	return &run{
		jd: domain.JobDescription{Title: "Backend Engineer", Text: "Backend Engineer\nWe are great.\n5+ years of Go.\nBonus: Kafka.\nOwn APIs."},
		sentences: []domain.ContextSentence{
			{Ref: "s1", Text: "Backend Engineer", Section: domain.SectionOther},
			{Ref: "s2", Text: "We are great.", Section: domain.SectionCompany},
			{Ref: "s3", Text: "5+ years of Go.", Section: domain.SectionRequired},
			{Ref: "s4", Text: "Bonus: Kafka.", Section: domain.SectionPreferred},
			{Ref: "s5", Text: "Own APIs.", Section: domain.SectionResponsibilities},
		},
	}
}

func TestJobSummaryGenerated(t *testing.T) {
	gen := &fakeGenerator{reply: "  A senior backend role building Go APIs.  "}
	m := metrics.NewRecorder()
	e := New(nil, gen, m, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Pipeline{})

	r := summaryRun()
	if err := e.jobSummary(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if r.summary != "A senior backend role building Go APIs." {
		t.Errorf("summary = %q", r.summary)
	}
	if !strings.Contains(gen.prompt, "5+ years of Go.") {
		t.Errorf("prompt does not include the posting: %q", gen.prompt)
	}
	if m.Summary().Counters["summary.fallback"] != 0 {
		t.Error("fallback counted for a generated summary")
	}
}

func TestJobSummaryFallback(t *testing.T) {
	tests := []struct {
		name string
		gen  port.AIGenerativeClient
	}{
		{"no generator", nil},
		{"not configured", &fakeGenerator{err: port.ErrGenerativeUnavailable}},
		{"call fails", &fakeGenerator{err: errors.New("HTTP 500")}},
		{"empty reply", &fakeGenerator{reply: "   "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := metrics.NewRecorder()
			e := New(nil, tt.gen, m, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Pipeline{})

			r := summaryRun()
			if err := e.jobSummary(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			want := "Backend Engineer. 5+ years of Go. Bonus: Kafka."
			if r.summary != want {
				t.Errorf("summary = %q, want %q", r.summary, want)
			}
			if m.Summary().Counters["summary.fallback"] != 1 {
				t.Error("fallback not counted")
			}
		})
	}
}

func TestJobSummaryTruncatesLongReply(t *testing.T) {
	gen := &fakeGenerator{reply: strings.Repeat("word ", 400)}
	e := New(nil, gen, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), config.Pipeline{})
	r := summaryRun()
	if err := e.jobSummary(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(r.summary)); n > maxSummaryRunes {
		t.Errorf("summary has %d runes, want <= %d", n, maxSummaryRunes)
	}
}
