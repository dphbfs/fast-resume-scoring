package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
	"github.com/dphbfs/fast-resume-scoring/tuning"
)

// ScoreApp runs the score command: the Match Score of a Resume for a Job
// Description, from one Holistic Round request.
type ScoreApp struct {
	judge    port.HolisticJudge
	recorder *metrics.Recorder
	log      *slog.Logger
	run      config.Run
	tuning   *tuning.Tuning
}

// NewScoreApp builds a ScoreApp.
func NewScoreApp(judge port.HolisticJudge, recorder *metrics.Recorder, log *slog.Logger, run config.Run, t *tuning.Tuning) *ScoreApp {
	return &ScoreApp{judge: judge, recorder: recorder, log: log, run: run, tuning: t}
}

// Run executes the command with args (without the program name) and returns
// the process exit code.
func (a *ScoreApp) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jdPath := fs.String("jd", "", "plain-text Job Description (.txt or .md)")
	resumePath := fs.String("resume", "", "Resume (.md, .txt, or .pdf)")
	out := fs.String("o", "", "write the score JSON to this file instead of stdout")
	quiet := fs.Bool("q", false, "don't print the run summary to stderr")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: score -jd job.txt -resume resume.md|resume.pdf [-o score.json] [-q]")
		fmt.Fprintln(stderr, "Sends the job description and resume text to the Jev provider at TYPESAFE_BASE_URL.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *jdPath == "" || *resumePath == "" || fs.NArg() != 0 {
		fs.Usage()
		return ExitUsage
	}
	jd, err := ReadJobDescription(*jdPath)
	if err != nil {
		fmt.Fprintln(stderr, "score:", err)
		return ExitUsage
	}
	resume, err := ReadResume(ctx, *resumePath)
	if err != nil {
		fmt.Fprintln(stderr, "score:", err)
		return ExitUsage
	}

	ctx, cancel := context.WithTimeout(ctx, a.run.Deadline)
	defer cancel()
	h, err := a.judge.Judge(ctx, jd, resume)
	err = deadlineError(err, a.run)
	if !*quiet {
		defer printSummary(a.recorder, a.log, stderr)
	}
	if err != nil {
		fmt.Fprintln(stderr, "score:", err)
		return ExitError
	}
	if err := writeJSON(domain.NewMatchResult(h, a.tuning.MatchWeights(), a.tuning.Hash), *out, stdout); err != nil {
		fmt.Fprintln(stderr, "score:", err)
		return ExitError
	}
	return ExitOK
}
