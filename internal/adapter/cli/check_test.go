package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

type fakeChecker struct {
	reqs   domain.Result
	resume domain.Resume
}

func (f *fakeChecker) Check(_ context.Context, reqs domain.Result, resume domain.Resume) (domain.CoverageResult, domain.CheckTrace, error) {
	f.reqs, f.resume = reqs, resume
	return domain.CoverageResult{
		SchemaVersion: domain.CoverageSchemaVersion,
		Requirements:  []domain.RequirementCoverage{{ID: "req_1", Value: "Go", Coverage: domain.StrengthStrong}},
	}, domain.CheckTrace{Units: []domain.TraceUnit{{ID: "e1", Text: "Built Go services"}}}, nil
}

func TestCheckRun(t *testing.T) {
	fc := &fakeChecker{}
	app := NewCheckApp(fc, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), testRun)
	reqs := writeFile(t, "result.json", `{"schema_version":"1","requirements":[{"id":"req_1","value":"Go","refs":["s1"]}],
		"alternative_groups":[],"context":{"s1":{"text":"Go.","section":"required"}}}`)
	resume := writeFile(t, "resume.md", "# Experience\n- Built Go services\n")
	trace := writeFile(t, "trace.json", "")

	var stdout, stderr bytes.Buffer
	code := app.Run(context.Background(), []string{"-q", "-requirements", reqs, "-resume", resume, "-debug", trace}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	if len(fc.reqs.Requirements) != 1 || fc.reqs.Context["s1"].Ref != "s1" {
		t.Errorf("requirements = %+v, want Context Refs restored", fc.reqs)
	}
	var res domain.CoverageResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil || res.Requirements[0].Coverage != domain.StrengthStrong {
		t.Errorf("stdout = %s (%v)", stdout.String(), err)
	}
	raw, _ := os.ReadFile(trace)
	var tr domain.CheckTrace
	if err := json.Unmarshal(raw, &tr); err != nil || len(tr.Units) != 1 {
		t.Errorf("trace = %s (%v)", raw, err)
	}
}

func TestCheckUsageErrors(t *testing.T) {
	app := NewCheckApp(&fakeChecker{}, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), testRun)
	resume := writeFile(t, "resume.md", "- Go\n")
	badSchema := writeFile(t, "result.json", `{"schema_version":"9"}`)
	for name, args := range map[string][]string{
		"no flags":    {},
		"no resume":   {"-requirements", badSchema},
		"bad schema":  {"-requirements", badSchema, "-resume", resume},
		"resume type": {"-requirements", badSchema, "-resume", badSchema},
		"extra arg":   {"-requirements", badSchema, "-resume", resume, "x"},
	} {
		t.Run(name, func(t *testing.T) {
			if code := app.Run(context.Background(), args, io.Discard, io.Discard); code != ExitUsage {
				t.Errorf("exit = %d, want %d", code, ExitUsage)
			}
		})
	}
}
