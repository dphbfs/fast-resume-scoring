package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

type fakeJudge struct {
	h   domain.Holistic
	err error
	jd  domain.JobDescription
}

func (f *fakeJudge) Judge(_ context.Context, jd domain.JobDescription, _ domain.Resume) (domain.Holistic, error) {
	f.jd = jd
	return f.h, f.err
}

func TestScoreRun(t *testing.T) {
	fj := &fakeJudge{h: domain.Holistic{Model: "jev-1.13", RoleMatch: 1}}
	app := NewScoreApp(fj, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	jd := writeFile(t, "job.txt", "# Backend Engineer\nBuild Go services.\n")
	resume := writeFile(t, "resume.md", "# Experience\n- Built Go services\n")

	var stdout, stderr bytes.Buffer
	if code := app.Run(context.Background(), []string{"-q", "-jd", jd, "-resume", resume}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	var res domain.MatchResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.SchemaVersion != "1" || res.Model != "jev-1.13" || res.MatchScore != 99 || res.Signals.RoleMatch != 1 || fj.jd.Title != "Backend Engineer" {
		t.Errorf("result = %+v, jd = %+v", res, fj.jd)
	}
}

func TestScoreErrors(t *testing.T) {
	jd := writeFile(t, "job.txt", "Engineer\n")
	resume := writeFile(t, "resume.md", "- Go\n")
	for name, tt := range map[string]struct {
		args []string
		err  error
		want int
	}{
		"missing resume": {[]string{"-jd", jd}, nil, ExitUsage},
		"bad jd ext":     {[]string{"-jd", resume + ".pdf", "-resume", resume}, nil, ExitUsage},
		"judge fails":    {[]string{"-q", "-jd", jd, "-resume", resume}, errors.New("jev down"), ExitError},
	} {
		app := NewScoreApp(&fakeJudge{err: tt.err}, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if got := app.Run(context.Background(), tt.args, io.Discard, io.Discard); got != tt.want {
			t.Errorf("%s: exit = %d, want %d", name, got, tt.want)
		}
	}
}
