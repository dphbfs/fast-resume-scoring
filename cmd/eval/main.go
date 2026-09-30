// Command eval runs the Requirement Extractor on the golden set against the
// live APIs and writes a dated report. It is not part of `go test`.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/eval"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/gencache"
)

func main() {
	os.Exit(run())
}

func run() int {
	golden := flag.String("golden", "testdata/golden", "directory with <id>.txt and <id>.expected.json")
	out := flag.String("out", "eval/reports", "directory for the dated JSON and Markdown report")
	parallel := flag.Int("parallel", 4, "fixtures extracted at once")
	only := flag.String("only", "", "comma-separated fixture ID prefixes to run")
	rescore := flag.String("rescore", "", "rescore the results in this report JSON against the current labels (no API calls)")
	summaries := flag.String("summaries", "eval/cache/summaries", "cache directory for generated Job Summaries (keeps Validation inputs fixed across runs)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var prefixes []string
	if *only != "" {
		prefixes = strings.Split(*only, ",")
	}
	fixtures, err := eval.LoadGolden(*golden, prefixes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		return 2
	}

	var report eval.Report
	if *rescore != "" {
		raw, err := os.ReadFile(*rescore)
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 2
		}
		var prev eval.Report
		if err := json.Unmarshal(raw, &prev); err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 2
		}
		traces, err := eval.LoadTraces(*rescore)
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 2
		}
		report = eval.Rescore(prev, fixtures, traces)
	} else {
		runner, err := initRunner(gencache.Dir(*summaries))
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 1
		}
		report = runner.Run(ctx, fixtures, *parallel)
	}

	path, err := report.Write(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		return 1
	}
	t := report.Totals
	fmt.Printf("recall %.1f%% (strict %.1f%%)  precision %.1f%%  F1 %.1f%%  filler %d  failed %d/%d\nreport: %s\n",
		100*t.RecallLoose, 100*t.RecallStrict, 100*t.PrecisionLoose, 100*t.F1Loose, t.FillerHits, t.Failed, t.Fixtures, path)
	if t.Failed > 0 {
		return 1
	}
	return 0
}
