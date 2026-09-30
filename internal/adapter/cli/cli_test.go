package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

type fakeExtractor struct {
	got domain.JobDescription
}

func (f *fakeExtractor) Extract(_ context.Context, jd domain.JobDescription) (domain.Result, error) {
	f.got = jd
	return domain.Result{
		SchemaVersion: domain.SchemaVersion,
		Requirements:  []domain.Requirement{{ID: "req_1", Value: "Go", Refs: []domain.Ref{"s1"}, Importance: 0.9}},
	}, nil
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunWritesResult(t *testing.T) {
	fx := &fakeExtractor{}
	app := New(fx, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	path := writeFile(t, "jd.md", "\n# Senior Go Engineer\n\nYou know Go.\n")

	var stdout, stderr bytes.Buffer
	if code := app.Run(context.Background(), []string{"-q", path}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	if fx.got.Title != "Senior Go Engineer" {
		t.Errorf("title = %q, want %q", fx.got.Title, "Senior Go Engineer")
	}
	var res domain.Result
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if len(res.Requirements) != 1 || res.Requirements[0].Value != "Go" {
		t.Errorf("result = %+v", res)
	}
}

func TestRunUsageErrors(t *testing.T) {
	app := New(&fakeExtractor{}, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct {
		name string
		args []string
	}{
		{"no file", nil},
		{"wrong extension", []string{writeFile(t, "jd.pdf", "x")}},
		{"empty file", []string{writeFile(t, "jd.txt", "  \n")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := app.Run(context.Background(), tt.args, &stdout, &stderr); code != ExitUsage {
				t.Fatalf("exit = %d, want %d", code, ExitUsage)
			}
		})
	}
}
