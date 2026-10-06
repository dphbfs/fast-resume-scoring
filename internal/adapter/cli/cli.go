// Package cli is the command-line driving adapter for the Requirement
// Extractor (extract), the Resume Checker (check), and the Match Score
// (score).
package cli

import (
	"context"
	"encoding/json"
	"errors"
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

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// MaxInputBytes caps a Job Description or Resume file. The Holistic Round
// sends both in one Jev request, which OpenRouter limits to 32k tokens.
const MaxInputBytes = 48 << 10

// App runs the extract command.
type App struct {
	extractor port.RequirementExtractor
	recorder  *metrics.Recorder
	log       *slog.Logger
	run       config.Run
}

// New builds an App.
func New(extractor port.RequirementExtractor, recorder *metrics.Recorder, log *slog.Logger, run config.Run) *App {
	return &App{extractor: extractor, recorder: recorder, log: log, run: run}
}

// Run executes the command with args (without the program name) and returns
// the process exit code.
func (a *App) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "write the result JSON to this file instead of stdout")
	quiet := fs.Bool("q", false, "don't print the run summary to stderr")
	debug := fs.String("debug", "", "write the extraction trace (dropped sentences, chunk choices, Filler, merges, probabilities) to this JSON file")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: extract [-o result.json] [-debug trace.json] [-q] <job-description.txt|.md>")
		fmt.Fprintln(stderr, "Sends the job description text to the Jev provider at TYPESAFE_BASE_URL and, when configured,")
		fmt.Fprintln(stderr, "to the generative provider at OPENAI_BASE_URL for the Job Summary.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return ExitUsage
	}

	jd, err := ReadJobDescription(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "extract:", err)
		return ExitUsage
	}

	ctx, cancel := context.WithTimeout(ctx, a.run.Deadline)
	defer cancel()
	result, trace, err := a.extractor.Extract(ctx, jd)
	err = deadlineError(err, a.run)
	if !*quiet {
		defer printSummary(a.recorder, a.log, stderr)
	}
	// The trace is written even when extraction fails: it shows how far the
	// pipeline got.
	if *debug != "" {
		if werr := writeJSON(trace, *debug, nil); werr != nil {
			fmt.Fprintln(stderr, "extract: debug:", werr)
			return ExitError
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "extract:", err)
		return ExitError
	}

	if err := writeJSON(result, *out, stdout); err != nil {
		fmt.Fprintln(stderr, "extract:", err)
		return ExitError
	}
	return ExitOK
}

func printSummary(recorder *metrics.Recorder, log *slog.Logger, w io.Writer) {
	fmt.Fprintln(w, "--- run summary ---")
	if err := recorder.Summary().WriteText(w); err != nil {
		log.Warn("write run summary", "error", err)
	}
}

// ReadJobDescription loads a .txt or .md file; the first non-empty line is
// the job title.
func ReadJobDescription(path string) (domain.JobDescription, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md":
	default:
		return domain.JobDescription{}, fmt.Errorf("%s: expected a .txt or .md file", path)
	}
	raw, err := readInput(path)
	if err != nil {
		return domain.JobDescription{}, err
	}
	text := string(raw)
	if strings.TrimSpace(text) == "" {
		return domain.JobDescription{}, errors.New(path + ": file is empty")
	}

	var title string
	for line := range strings.Lines(text) {
		if t := strings.TrimSpace(strings.TrimLeft(line, "# ")); t != "" {
			title = t
			break
		}
	}
	return domain.JobDescription{Title: title, Text: text}, nil
}

// readInput reads a text input file of at most MaxInputBytes.
func readInput(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(raw) > MaxInputBytes {
		return nil, fmt.Errorf("%s: larger than the %d KiB input limit", path, MaxInputBytes>>10)
	}
	return raw, nil
}

// deadlineError names the run deadline when err comes from it.
func deadlineError(err error, run config.Run) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("run deadline exceeded (RUN_DEADLINE=%s): %w", run.Deadline, err)
	}
	return err
}

// writeJSON writes v as indented JSON to path, or to stdout when path is empty.
func writeJSON(v any, path string, stdout io.Writer) error {
	w := stdout
	if path != "" {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
