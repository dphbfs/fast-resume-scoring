package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev/jevtest"
	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

func newTestJudge(t *testing.T, srvURL string) *HolisticJudge {
	t.Helper()
	m := metrics.NewRecorder()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := jev.New(config.Jev{APIKey: "k", BaseURL: srvURL, Model: "jev-latest", MaxConcurrency: 1, Timeout: 5 * time.Second}, m, log)
	if err != nil {
		t.Fatal(err)
	}
	return NewHolisticJudge(c, m, log)
}

func TestHolisticJudge(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(id string, q jev.WireQuestion) jev.WireAnswer {
		switch id {
		case "responsibilities":
			return jevtest.Score(2, 0.6, map[string]float64{"2": 1})
		case "primary_gap":
			return jevtest.Noul(0.7)
		}
		return jevtest.Noul(0.05)
	}))
	jd := domain.JobDescription{Title: "Engineer", Text: "## Acme - Engineer\n\n### Stack & Responsibilities\n- Go\n\n### Full Job Description\nBuild Go services.\n"}
	j := newTestJudge(t, srv.URL)
	j.now = func() time.Time { return time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC) }
	resume := "# Experience\n## Engineer | Acme | Jun 2025 – Nov 2025\n- Built Go services\n"
	h, err := j.Judge(context.Background(), jd, domain.Resume{Text: resume})
	if err != nil {
		t.Fatal(err)
	}
	if h.Blocker != 0.05 || h.Responsibilities != 0.5 || h.PrimaryGap != 0.7 || h.DomainMismatch != 0.05 ||
		h.LocationMismatch != 0.05 || h.GapMonths == nil || *h.GapMonths != 11 {
		t.Errorf("holistic = %+v", h)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || len(reqs[0].Questions) != 5 {
		t.Fatalf("requests = %+v", reqs)
	}
	state, _ := json.Marshal(reqs[0].State)
	if strings.Contains(string(state), "Stack & Responsibilities") || !strings.Contains(string(state), "Build Go services") {
		t.Errorf("state should hold the full posting only: %s", state)
	}
}

func TestHolisticJudgeRequiresAnswers(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(id string, _ jev.WireQuestion) jev.WireAnswer {
		switch id {
		case "blocker":
			return jev.WireAnswer{Type: "noul"}
		case "responsibilities":
			return jevtest.Score(1, 1, map[string]float64{"1": 1})
		}
		return jevtest.Noul(0)
	}))
	if _, err := newTestJudge(t, srv.URL).Judge(context.Background(), domain.JobDescription{Text: "x"}, domain.Resume{Text: "y"}); err == nil {
		t.Error("want error for a missing blocker answer")
	}
}

func TestEmploymentGapMonths(t *testing.T) {
	asOf := time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		resume string
		want   *int
	}{
		{"latest end", "# Experience\n## A | X | Mar 2022 – Apr 2025\n- a\n## B | Y | Jun 2025 - Nov 2025\n- b\n", ptrInt(11)},
		{"current role", "# Experience\n## A | X | Mar 2022 – Present\n- a\n", ptrInt(0)},
		{"year only", "# Experience\n## A | X | 2019 – 2024\n- a\n", ptrInt(22)},
		{"education ignored", "# Education\n## BSc | Uni | 2009 – 2013\n- cs\n", nil},
		{"no dates", "# Experience\n- a\n", nil},
	} {
		got := employmentGapMonths(ParseResume(tt.resume), asOf)
		if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
			t.Errorf("%s: gap = %v, want %v", tt.name, derefInt(got), derefInt(tt.want))
		}
	}
}

func ptrInt(n int) *int { return &n }

func derefInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
