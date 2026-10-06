package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// CheckApp runs the check command: the Resume Checker on an extract result
// and a Resume file.
type CheckApp struct {
	checker  port.ResumeChecker
	recorder *metrics.Recorder
	log      *slog.Logger
	run      config.Run
}

// NewCheckApp builds a CheckApp.
func NewCheckApp(checker port.ResumeChecker, recorder *metrics.Recorder, log *slog.Logger, run config.Run) *CheckApp {
	return &CheckApp{checker: checker, recorder: recorder, log: log, run: run}
}

// Run executes the command with args (without the program name) and returns
// the process exit code.
func (a *CheckApp) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reqPath := fs.String("requirements", "", "extract result JSON (schema v1) with the Requirements to check")
	resumePath := fs.String("resume", "", "plain-text Resume (.md or .txt) in the markdown convention")
	out := fs.String("o", "", "write the coverage JSON to this file instead of stdout")
	quiet := fs.Bool("q", false, "don't print the run summary to stderr")
	debug := fs.String("debug", "", "write the check trace (retrieval options, strength probabilities, accepted and rejected pairs) to this JSON file")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: check -requirements result.json -resume resume.md [-o coverage.json] [-debug trace.json] [-q]")
		fmt.Fprintln(stderr, "Sends the resume text and the Requirements to the Jev provider at TYPESAFE_BASE_URL.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *reqPath == "" || *resumePath == "" || fs.NArg() != 0 {
		fs.Usage()
		return ExitUsage
	}

	reqs, err := readResult(*reqPath)
	if err != nil {
		fmt.Fprintln(stderr, "check:", err)
		return ExitUsage
	}
	resume, err := ReadResume(*resumePath)
	if err != nil {
		fmt.Fprintln(stderr, "check:", err)
		return ExitUsage
	}

	ctx, cancel := context.WithTimeout(ctx, a.run.Deadline)
	defer cancel()
	result, trace, err := a.checker.Check(ctx, reqs, resume)
	err = deadlineError(err, a.run)
	if !*quiet {
		defer printSummary(a.recorder, a.log, stderr)
	}
	if *debug != "" {
		if werr := writeJSON(trace, *debug, nil); werr != nil {
			fmt.Fprintln(stderr, "check: debug:", werr)
			return ExitError
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "check:", err)
		return ExitError
	}
	if err := writeJSON(result, *out, stdout); err != nil {
		fmt.Fprintln(stderr, "check:", err)
		return ExitError
	}
	return ExitOK
}

// readResult loads an extract result (schema v1).
func readResult(path string) (domain.Result, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.Result{}, err
	}
	var res domain.Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return domain.Result{}, fmt.Errorf("%s: %w", path, err)
	}
	if res.SchemaVersion != domain.SchemaVersion {
		return domain.Result{}, fmt.Errorf("%s: schema_version %q, want %q", path, res.SchemaVersion, domain.SchemaVersion)
	}
	for ref, s := range res.Context {
		s.Ref = ref
		res.Context[ref] = s
	}
	return res, nil
}

// ReadResume loads a .md or .txt Resume.
func ReadResume(path string) (domain.Resume, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md":
	default:
		return domain.Resume{}, fmt.Errorf("%s: expected a .md or .txt file", path)
	}
	raw, err := readInput(path)
	if err != nil {
		return domain.Resume{}, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return domain.Resume{}, fmt.Errorf("%s: file is empty", path)
	}
	return domain.Resume{Text: string(raw)}, nil
}
