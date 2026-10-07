package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-scoring/tuning"
)

// testRun is a run deadline long enough for any test.
var testRun = config.Run{Deadline: time.Minute}

type fakeJudge struct {
	h     domain.Holistic
	err   error
	jd    domain.JobDescription
	block bool // wait for the context to end, as a hung Jev call would
}

func (f *fakeJudge) Judge(ctx context.Context, jd domain.JobDescription, _ domain.Resume) (domain.Holistic, error) {
	f.jd = jd
	if f.block {
		<-ctx.Done()
		return domain.Holistic{}, ctx.Err()
	}
	return f.h, f.err
}

func TestScoreRun(t *testing.T) {
	fj := &fakeJudge{h: domain.Holistic{Model: "jev-1.13", RoleMatch: 1}}
	app := NewScoreApp(fj, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), testRun, tuning.Default())
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
		app := NewScoreApp(&fakeJudge{err: tt.err}, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), testRun, tuning.Default())
		if got := app.Run(context.Background(), tt.args, io.Discard, io.Discard); got != tt.want {
			t.Errorf("%s: exit = %d, want %d", name, got, tt.want)
		}
	}
}

func TestScoreRunDeadline(t *testing.T) {
	app := NewScoreApp(&fakeJudge{block: true}, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)),
		config.Run{Deadline: 20 * time.Millisecond}, tuning.Default())
	jd := writeFile(t, "job.txt", "Engineer\n")
	resume := writeFile(t, "resume.md", "- Go\n")
	var stderr bytes.Buffer
	start := time.Now()
	if code := app.Run(context.Background(), []string{"-q", "-jd", jd, "-resume", resume}, io.Discard, &stderr); code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("run took %s, want it bounded by the deadline", elapsed)
	}
	if !strings.Contains(stderr.String(), "run deadline exceeded (RUN_DEADLINE=20ms)") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestReadInputLimit(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "job.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", MaxInputBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJobDescription(big); err == nil || !strings.Contains(err.Error(), "48 KiB input limit") {
		t.Errorf("err = %v, want input limit error", err)
	}
	exact := filepath.Join(dir, "resume.md")
	if err := os.WriteFile(exact, []byte(strings.Repeat("a", MaxInputBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResume(exact); err != nil {
		t.Errorf("resume at the limit: %v", err)
	}
}
