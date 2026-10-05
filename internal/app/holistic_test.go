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
		case "core_work":
			return jevtest.Score(3, 0.6, map[string]float64{"3": 0.6, "4": 0.4})
		}
		return jevtest.Noul(0.05)
	}))
	jd := domain.JobDescription{Title: "Engineer", Text: "## Acme - Engineer\n\n### Stack & Responsibilities\n- Go\n\n### Full Job Description\nBuild Go services.\n"}
	h, err := newTestJudge(t, srv.URL).Judge(context.Background(), jd, domain.Resume{Text: "- Built Go services\n"})
	if err != nil {
		t.Fatal(err)
	}
	if h.CoreWork != 0.75 || h.Blocker != 0.05 {
		t.Errorf("holistic = %+v", h)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || len(reqs[0].Questions) != 2 {
		t.Fatalf("requests = %+v", reqs)
	}
	state, _ := json.Marshal(reqs[0].State)
	if strings.Contains(string(state), "Stack & Responsibilities") || !strings.Contains(string(state), "Build Go services") {
		t.Errorf("state should hold the full posting only: %s", state)
	}
}

func TestHolisticJudgeRequiresAnswers(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(id string, _ jev.WireQuestion) jev.WireAnswer {
		if id == "blocker" {
			return jev.WireAnswer{Type: "noul"}
		}
		return jevtest.Score(1, 1, map[string]float64{"1": 1})
	}))
	if _, err := newTestJudge(t, srv.URL).Judge(context.Background(), domain.JobDescription{Text: "x"}, domain.Resume{Text: "y"}); err == nil {
		t.Error("want error for a missing blocker answer")
	}
}
