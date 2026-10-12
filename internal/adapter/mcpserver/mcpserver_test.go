package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/cli"
	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/tuning"
)

type fakeJudge struct{ jd domain.JobDescription }

func (f *fakeJudge) Judge(_ context.Context, jd domain.JobDescription, _ domain.Resume) (domain.Holistic, error) {
	f.jd = jd
	return domain.Holistic{Model: "jev-1.13", RoleMatch: 1}, nil
}

type fakeExtractor struct{ res domain.Result }

func (f fakeExtractor) Extract(context.Context, domain.JobDescription) (domain.Result, domain.Trace, error) {
	return f.res, domain.Trace{}, nil
}

type fakeChecker struct{ reqs domain.Result }

func (f *fakeChecker) Check(_ context.Context, reqs domain.Result, _ domain.Resume) (domain.CoverageResult, domain.CheckTrace, error) {
	f.reqs = reqs
	return domain.CoverageResult{SchemaVersion: "1"}, domain.CheckTrace{}, nil
}

func exampleResult(t *testing.T) domain.Result {
	t.Helper()
	raw, err := os.ReadFile("../../../examples/requirements.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := cli.ParseResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func connect(t *testing.T, s *Server) *mcp.ClientSession {
	t.Helper()
	ctx := t.Context()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.MCPServer("test").Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestTools(t *testing.T) {
	judge := &fakeJudge{}
	checker := &fakeChecker{}
	reqs := exampleResult(t)
	s := New(judge, fakeExtractor{res: reqs}, checker, slog.New(slog.DiscardHandler), config.Run{Deadline: time.Minute}, tuning.Default())
	cs := connect(t, s)

	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, ","); got != "check,extract,score" {
		t.Fatalf("tools = %s", got)
	}

	res := call(t, cs, "score", map[string]any{"job_description": "\n# Senior Go Engineer\nBuild things.", "resume": "Go dev"})
	if res.IsError {
		t.Fatalf("score: %v", res.Content)
	}
	var m domain.MatchResult
	if err := remarshal(res.StructuredContent, &m); err != nil {
		t.Fatal(err)
	}
	if m.Model != "jev-1.13" || judge.jd.Title != "Senior Go Engineer" {
		t.Fatalf("score = %+v, title %q", m, judge.jd.Title)
	}

	res = call(t, cs, "extract", map[string]any{"job_description": "Go engineer"})
	if res.IsError {
		t.Fatalf("extract: %v", res.Content)
	}

	// check accepts extract's structured output unchanged.
	res = call(t, cs, "check", map[string]any{"requirements": res.StructuredContent, "resume": "Go dev"})
	if res.IsError {
		t.Fatalf("check: %v", res.Content)
	}
	if len(checker.reqs.Requirements) != len(reqs.Requirements) {
		t.Fatalf("check got %d requirements, want %d", len(checker.reqs.Requirements), len(reqs.Requirements))
	}
}

func TestToolInputErrors(t *testing.T) {
	s := New(&fakeJudge{}, fakeExtractor{}, &fakeChecker{}, slog.New(slog.DiscardHandler), config.Run{Deadline: time.Minute}, tuning.Default())
	cs := connect(t, s)
	for _, tt := range []struct {
		name, tool string
		args       map[string]any
		want       string
	}{
		{"empty resume", "score", map[string]any{"job_description": "x", "resume": " "}, "resume is empty"},
		{"too large", "extract", map[string]any{"job_description": strings.Repeat("a", cli.MaxInputBytes+1)}, "input limit"},
		{"bad schema", "check", map[string]any{"requirements": map[string]any{"schema_version": "0", "model": "m", "requirements": []any{}, "alternative_groups": []any{}, "context": map[string]any{}}, "resume": "x"}, "schema_version"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := call(t, cs, tt.tool, tt.args)
			if !res.IsError {
				t.Fatal("want a tool error")
			}
			if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, tt.want) {
				t.Fatalf("error %q, want %q", text, tt.want)
			}
		})
	}
}

func remarshal(from, to any) error {
	raw, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, to)
}
