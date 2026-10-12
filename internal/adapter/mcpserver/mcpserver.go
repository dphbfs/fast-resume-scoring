// Package mcpserver is the Model Context Protocol driving adapter: it
// exposes the Match Score (score), the Requirement Extractor (extract), and
// the Resume Checker (check) as MCP tools over stdio or streamable HTTP.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/cli"
	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
	"github.com/dphbfs/fast-resume-scoring/tuning"
)

// Name is the MCP implementation name.
const Name = "fast-resume-scoring"

// Server holds the application ports the MCP tools call.
type Server struct {
	judge     port.HolisticJudge
	extractor port.RequirementExtractor
	checker   port.ResumeChecker
	log       *slog.Logger
	run       config.Run
	tuning    *tuning.Tuning
}

// New builds a Server.
func New(
	judge port.HolisticJudge,
	extractor port.RequirementExtractor,
	checker port.ResumeChecker,
	log *slog.Logger,
	run config.Run,
	t *tuning.Tuning,
) *Server {
	return &Server{judge: judge, extractor: extractor, checker: checker, log: log.With("component", "mcp"), run: run, tuning: t}
}

// ScoreInput is the score tool's input.
type ScoreInput struct {
	JobDescription string `json:"job_description" jsonschema:"full job posting text (plain text or Markdown); the first non-empty line is the job title"`
	Resume         string `json:"resume" jsonschema:"resume text (plain text or Markdown)"`
}

// ExtractInput is the extract tool's input.
type ExtractInput struct {
	JobDescription string `json:"job_description" jsonschema:"full job posting text (plain text or Markdown); the first non-empty line is the job title"`
}

// CheckInput is the check tool's input.
type CheckInput struct {
	Requirements domain.Result `json:"requirements" jsonschema:"the extract tool's result, unchanged"`
	Resume       string        `json:"resume" jsonschema:"resume text; parses best with '# Experience' and '## Role | Company | YYYY-MM – YYYY-MM' headings"`
}

// Tool outputs are untyped (any) so the SDK publishes no output schema: the
// result schemas carry nullable maps and slices that a generated schema
// rejects. Results still go out as structured content.

// MCPServer builds the MCP server with the three tools registered. version
// is the release version reported to clients.
func (s *Server) MCPServer(version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: Name, Version: version}, &mcp.ServerOptions{
		Instructions: "Score how well a resume fits a job posting with Jev, a typed classifier. " +
			"Use score for a 0-100 Match Score. To explain it, call extract on the posting, " +
			"then check with that result and the resume to get Coverage per Requirement, Gaps, and a Fit Score.",
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(true)}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "score",
		Title:       "Match Score",
		Description: "Match Score (0-100) of a resume for a job posting, with the three signals behind it: role_match, experience_short, blocker. One Jev request, under a second.",
		Annotations: readOnly,
	}, s.score)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "extract",
		Title:       "Extract Requirements",
		Description: "Turns a job posting into atomic Requirements, each with a Tier (required, preferred, mentioned), Importance, Alternative Groups, and the sentences it came from. Pass the result to check.",
		Annotations: readOnly,
	}, s.extract)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "check",
		Title:       "Check Resume Coverage",
		Description: "Links resume lines to the Requirements from extract and returns Coverage per Requirement (strong, partial, weak, none) with quoted evidence, the Gaps in required items, and a Fit Score.",
		Annotations: readOnly,
	}, s.check)
	return srv
}

func (s *Server) score(ctx context.Context, _ *mcp.CallToolRequest, in ScoreInput) (*mcp.CallToolResult, any, error) {
	jd, err := jobDescription(in.JobDescription)
	if err != nil {
		return nil, nil, err
	}
	resume, err := resumeText(in.Resume)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.run.Deadline)
	defer cancel()
	h, err := s.judge.Judge(ctx, jd, resume)
	if err != nil {
		return nil, nil, s.fail("score", err)
	}
	return nil, domain.NewMatchResult(h, s.tuning.MatchWeights(), s.tuning.Hash), nil
}

func (s *Server) extract(ctx context.Context, _ *mcp.CallToolRequest, in ExtractInput) (*mcp.CallToolResult, any, error) {
	jd, err := jobDescription(in.JobDescription)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.run.Deadline)
	defer cancel()
	res, _, err := s.extractor.Extract(ctx, jd)
	if err != nil {
		return nil, nil, s.fail("extract", err)
	}
	return nil, res, nil
}

func (s *Server) check(ctx context.Context, _ *mcp.CallToolRequest, in CheckInput) (*mcp.CallToolResult, any, error) {
	reqs, err := cli.PrepareResult(in.Requirements)
	if err != nil {
		return nil, nil, fmt.Errorf("requirements: %w", err)
	}
	resume, err := resumeText(in.Resume)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.run.Deadline)
	defer cancel()
	res, _, err := s.checker.Check(ctx, reqs, resume)
	if err != nil {
		return nil, nil, s.fail("check", err)
	}
	return nil, res, nil
}

// fail logs a tool error and names the run deadline when err comes from it.
func (s *Server) fail(tool string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("run deadline exceeded (RUN_DEADLINE=%s): %w", s.run.Deadline, err)
	}
	s.log.Warn("tool failed", "tool", tool, "error", err)
	return err
}

func jobDescription(text string) (domain.JobDescription, error) {
	if err := checkText("job_description", text); err != nil {
		return domain.JobDescription{}, err
	}
	return cli.ParseJobDescription(text), nil
}

func resumeText(text string) (domain.Resume, error) {
	if err := checkText("resume", text); err != nil {
		return domain.Resume{}, err
	}
	return domain.Resume{Text: text}, nil
}

// checkText applies the CLI's input rules: not empty, at most
// cli.MaxInputBytes.
func checkText(field, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if len(text) > cli.MaxInputBytes {
		return fmt.Errorf("%s is larger than the %d KiB input limit", field, cli.MaxInputBytes>>10)
	}
	return nil
}
