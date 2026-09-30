// Package cli is the command-line driving adapter for the Requirement
// Extractor.
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
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// App runs the extract command.
type App struct {
	extractor port.RequirementExtractor
	recorder  *metrics.Recorder
	log       *slog.Logger
}

// New builds an App.
func New(extractor port.RequirementExtractor, recorder *metrics.Recorder, log *slog.Logger) *App {
	return &App{extractor: extractor, recorder: recorder, log: log}
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

	result, trace, err := a.extractor.Extract(ctx, jd)
	if !*quiet {
		defer a.printSummary(stderr)
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

func (a *App) printSummary(w io.Writer) {
	fmt.Fprintln(w, "--- run summary ---")
	if err := a.recorder.Summary().WriteText(w); err != nil {
		a.log.Warn("write run summary", "error", err)
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
	raw, err := os.ReadFile(path)
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
