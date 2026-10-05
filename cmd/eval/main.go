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
	checker := flag.Bool("checker", false, "evaluate the Resume Checker on testdata/checker pairs instead of the Requirement Extractor")
	baseline := flag.Bool("baseline", false, "with -checker: also score each pair with one generative prompt (OPENAI_*) and compare")
	priceIn := flag.Float64("baseline-price-in", 0, "baseline input price, USD per million tokens (used when the provider reports no cost; with -rescore, reprices stored calls)")
	priceOut := flag.Float64("baseline-price-out", 0, "baseline output price, USD per million tokens (used when the provider reports no cost)")
	e2e := flag.Bool("e2e", false, "evaluate extraction + checking end to end against the reference scores in testdata/reference")
	set := flag.String("set", eval.E2ESubset, "with -e2e: pairs to run: subset, current, or all")
	extractCache := flag.String("extract-cache", "eval/cache/extract", "with -e2e: cache directory for extraction results (empty: always extract, for cold cost)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var prefixes []string
	if *only != "" {
		prefixes = strings.Split(*only, ",")
	}
	if *e2e {
		dir := *out
		if dir == "eval/reports" {
			dir = "eval/reports/e2e"
		}
		return runE2E(ctx, *set, dir, *summaries, *extractCache, prefixes, *parallel)
	}
	if *checker {
		dir := *out
		if dir == "eval/reports" {
			dir = "eval/reports/checker"
		}
		bcfg := eval.BaselineConfig{Enabled: *baseline, PriceInPerM: *priceIn, PriceOutPerM: *priceOut}
		return runChecker(ctx, *golden, dir, *rescore, prefixes, *parallel, bcfg)
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

// runChecker evaluates the Resume Checker on the (Job Description, Resume)
// pairs in testdata/checker, or rescores a previous checker report.
func runChecker(ctx context.Context, golden, out, rescore string, prefixes []string, parallel int, baseline eval.BaselineConfig) int {
	fixtures, err := eval.LoadCheckerGolden("testdata/checker", golden, "testdata/resumes", prefixes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		return 2
	}
	var report eval.CheckerReport
	if rescore != "" {
		raw, err := os.ReadFile(rescore)
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 2
		}
		var prev eval.CheckerReport
		if err := json.Unmarshal(raw, &prev); err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 2
		}
		if baseline.PriceInPerM > 0 || baseline.PriceOutPerM > 0 {
			eval.RepriceBaseline(&prev, baseline)
		}
		traces, err := eval.LoadCheckTraces(rescore)
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 2
		}
		if report, err = eval.RescoreChecker(prev, fixtures, traces); err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 2
		}
	} else {
		runner, err := initCheckerRunner(baseline)
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			return 1
		}
		report = runner.Run(ctx, fixtures, parallel)
	}

	path, err := report.Write(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		return 1
	}
	t := report.Totals
	fmt.Printf("coverage %.1f%%  retrieval recall %.1f%%  links P %.1f%% R %.1f%%  strength exact %.1f%%  failed %d/%d\nreport: %s\n",
		100*t.CoverageExact, 100*t.RetrievalRecall, 100*t.LinkPrecision, 100*t.LinkRecall, 100*t.StrengthExact, t.Failed, t.Fixtures, path)
	if b := t.Baseline; b != nil {
		fmt.Printf("fit error vs labels over %d pairs: generative %.1f (max %d)  jev %.1f (max %d)  baseline failed %d/%d\n",
			b.Pairs, b.FitError, b.FitErrorMax, b.JevFitError, b.JevFitErrorMax, b.Failed, b.Ran)
	}
	if t.Failed > 0 {
		return 1
	}
	return 0
}

// runE2E extracts and checks the reference pairs and compares each Fit
// Score with the reference score.
func runE2E(ctx context.Context, set, out, summaries, extractCache string, prefixes []string, parallel int) int {
	pairs, err := eval.LoadE2E("testdata/reference", ".", set, prefixes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		return 2
	}
	runner, err := initE2ERunner(gencache.Dir(summaries))
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		return 1
	}
	report := runner.Run(ctx, pairs, parallel, extractCache)
	report.Set = set
	path, err := report.Write(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		return 1
	}
	t := report.Totals
	fmt.Printf("fit: MAE %.1f  bias %+.1f  within10 %.0f%%  max %.0f  tau-b %.2f  saved tau-b %.2f\n",
		t.MAE, t.Bias, 100*t.Within10, t.MaxError, t.TauB, t.SavedTauB)
	if m := t.Match; m != nil {
		fmt.Printf("match: MAE %.1f  bias %+.1f  within10 %.0f%%  max %.0f  tau-b %.2f\n", m.MAE, m.Bias, 100*m.Within10, m.MaxError, m.TauB)
	}
	fmt.Printf("$%.4f/pair  failed %d/%d\nreport: %s\n", t.JevCostPerPair, t.Failed, t.Pairs, path)
	if t.Failed > 0 {
		return 1
	}
	return 0
}
