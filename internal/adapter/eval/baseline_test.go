package eval

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/openai"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

type fakeCompleter struct {
	reply  openai.Reply
	err    error
	prompt string
}

func (f *fakeCompleter) Complete(_ context.Context, system, prompt string) (openai.Reply, error) {
	if system != "" {
		return openai.Reply{}, errors.New("baseline must not send a system message")
	}
	f.prompt = prompt
	return f.reply, f.err
}

func TestParseBaselineReply(t *testing.T) {
	nine := `["a","b","c","d","e","f","g","h","i"]`
	tests := []struct {
		name      string
		text      string
		score     int
		gaps      int
		strengths int
		wantErr   bool
	}{
		{"plain", `{"score": 72, "gaps": ["Kafka"], "strengths": ["Go", "AWS"]}`, 72, 1, 2, false},
		{"fenced with prose", "Here you go:\n```json\n{\"score\": 64.6, \"gaps\": [], \"strengths\": []}\n```", 65, 0, 0, false},
		{"clamped", `{"score": 140}`, 100, 0, 0, false},
		{"string score, coerced like Reactive Resume", `{"score": "55"}`, 55, 0, 0, false},
		{"missing score", `{"gaps": []}`, 0, 0, 0, true},
		{"capped lists", `{"score": 10, "gaps": ` + nine + `, "strengths": ` + nine + `}`, 10, 8, 8, false},
		{"no json", "I cannot score this.", 0, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, gaps, strengths, err := parseBaselineReply(tt.text)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if score != tt.score || len(gaps) != tt.gaps || len(strengths) != tt.strengths {
				t.Errorf("got %d, %d gaps, %d strengths; want %d, %d, %d", score, len(gaps), len(strengths), tt.score, tt.gaps, tt.strengths)
			}
		})
	}
}

func TestBaselineScore(t *testing.T) {
	gen := &fakeCompleter{reply: openai.Reply{
		Text:    `{"score": 80, "gaps": ["Kubernetes"], "strengths": ["Go"]}`,
		Usage:   openai.Usage{InputTokens: 2000, OutputTokens: 100},
		Latency: 1500 * time.Millisecond,
	}}
	b := &Baseline{gen: gen, cfg: BaselineConfig{Enabled: true, PriceInPerM: 5, PriceOutPerM: 25}}
	s := b.Score(context.Background(), "JD TEXT", "RESUME TEXT")
	if s.Error != "" || s.Score == nil || *s.Score != 80 || !slices.Equal(s.Gaps, []string{"Kubernetes"}) {
		t.Fatalf("score = %+v", s)
	}
	if s.CostUSD == nil || *s.CostUSD != 0.0125 || s.DurationMS != 1500 {
		t.Errorf("cost = %v, duration = %d; want 0.0125 (2000×5 + 100×25 per M), 1500", s.CostUSD, s.DurationMS)
	}
	if i, j := strings.Index(gen.prompt, "RESUME TEXT"), strings.Index(gen.prompt, "JD TEXT"); i < 0 || j < i {
		t.Errorf("prompt must carry the Resume, then the Job Description: %q", gen.prompt)
	}

	gen.reply.Usage.InputTokens = 2 // a proxy that misreports input
	if s := b.Score(context.Background(), strings.Repeat("x", 4000), "cv"); !s.InputTokensEstimated || s.InputTokens < 1000 {
		t.Errorf("input = %d (estimated %v), want a length estimate", s.InputTokens, s.InputTokensEstimated)
	}

	reported := 0.002
	gen.reply.Usage.Cost = &reported
	if s := b.Score(context.Background(), "jd", "cv"); *s.CostUSD != reported {
		t.Errorf("cost = %v, want the provider-reported %v", *s.CostUSD, reported)
	}

	gen.err = errors.New("HTTP 502")
	if s := b.Score(context.Background(), "jd", "cv"); s.Error == "" || s.Score != nil {
		t.Errorf("failed call = %+v, want Error set and no score", s)
	}
}

func TestNewBaselineDisabled(t *testing.T) {
	if b := NewBaseline(nil, config.Generative{Model: "m"}, BaselineConfig{}); b != nil {
		t.Errorf("disabled baseline = %+v, want nil", b)
	}
}

func TestCheckerRunWithBaseline(t *testing.T) {
	f := checkerFixture()
	gen := &fakeCompleter{reply: openai.Reply{
		Text:  `{"score": 90, "gaps": [], "strengths": ["Go"]}`,
		Usage: openai.Usage{InputTokens: 1000, OutputTokens: 50},
	}}
	b := &Baseline{gen: gen, cfg: BaselineConfig{Enabled: true, Model: "gen-m", PriceInPerM: 1}}
	r := NewCheckerRunner(echoChecker{}, b, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), config.Checker{}, tuning.Default())
	rep := r.Run(context.Background(), []CheckerFixture{f}, 1)

	bt := rep.Totals.Baseline
	if bt == nil || bt.Ran != 1 || bt.Pairs != 1 || bt.Failed != 0 || rep.Baseline == nil || rep.Baseline.Model != "gen-m" {
		t.Fatalf("baseline totals = %+v, config = %+v", bt, rep.Baseline)
	}
	s := rep.Fixtures[0]
	want := abs(90 - *s.FitWant)
	if bt.FitError != float64(want) || bt.JevFitError != float64(abs(*s.FitGot-*s.FitWant)) {
		t.Errorf("fit errors = %v (jev %v), want %d", bt.FitError, bt.JevFitError, want)
	}
	if bt.CostKnown != 1 || bt.CostUSD != 0.001 {
		t.Errorf("cost = %v of %d, want 0.001 of 1", bt.CostUSD, bt.CostKnown)
	}

	md, err := rep.Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(md)
	for _, want := range []string{"## Generative baseline (`gen-m`)", "| Fit error vs labeled, mean / max (1 pairs) |"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("markdown lacks %q:\n%s", want, text)
		}
	}

	RepriceBaseline(&rep, BaselineConfig{PriceInPerM: 5, PriceOutPerM: 25})
	if c := rep.Fixtures[0].Baseline.CostUSD; c == nil || *c != 0.00625 {
		t.Errorf("repriced cost = %v, want 0.00625 (1000×5 + 50×25 per M)", c)
	}
	re, err := RescoreChecker(rep, []CheckerFixture{f}, nil, tuning.Default())
	if err != nil {
		t.Fatal(err)
	}
	if re.Totals.Baseline == nil || re.Totals.Baseline.FitError != bt.FitError || re.Totals.Baseline.CostUSD != 0.00625 {
		t.Errorf("rescore lost the baseline: %+v", re.Totals.Baseline)
	}
}
