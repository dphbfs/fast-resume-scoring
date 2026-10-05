// Package port declares the interfaces the application core depends on.
// Adapters in internal/adapter implement them.
package port

import (
	"context"
	"errors"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

// RequirementExtractor is the driving port: the CLI (and later the
// resume-tailoring backend) calls it to extract Requirements. The Trace
// records intermediate decisions for debugging and eval; callers that do
// not need it ignore it.
type RequirementExtractor interface {
	Extract(ctx context.Context, jd domain.JobDescription) (domain.Result, domain.Trace, error)
}

// ResumeChecker is the driving port that links a Resume's Evidence Units
// to extracted Requirements and reports their Coverage. The CheckTrace
// records intermediate decisions; callers that do not need it ignore it.
type ResumeChecker interface {
	Check(ctx context.Context, reqs domain.Result, resume domain.Resume) (domain.CoverageResult, domain.CheckTrace, error)
}

// HolisticJudge is the driving port that judges a whole Resume against a
// whole posting (the Holistic Round), independently of extraction.
type HolisticJudge interface {
	Judge(ctx context.Context, jd domain.JobDescription, resume domain.Resume) (domain.Holistic, error)
}

// QuestionType is the kind of judgment a classifier question asks for.
type QuestionType string

const (
	// Noul is a yes/no question; the answer is P(yes).
	Noul QuestionType = "noul"
	// Choice picks one option from a map of option -> description.
	Choice QuestionType = "choice"
	// Score places the state on ordered levels.
	Score QuestionType = "score"
)

// Question is one typed question about a classification state.
//
// Criteria depends on Type: optional map[string]any{"true": ..., "false": ...}
// for Noul, map[string]any (option -> description or nil) for Choice, and an
// ordered []any of level descriptions for Score.
type Question struct {
	Type         QuestionType
	Instructions any
	Criteria     any
}

// ClassifyRequest evaluates every question against one state, in parallel.
type ClassifyRequest struct {
	// State is a string, or JSON-serializable object or array.
	State     any
	Questions map[string]Question
}

// Answer is the typed answer to one Question. Which fields are set depends
// on Type; pointers distinguish "absent" from zero.
type Answer struct {
	Type          QuestionType
	Noul          *float64
	Choice        string
	Score         *float64
	Probabilities map[string]float64
	Confidence    *float64
}

// Usage is the token usage of one classifier call.
type Usage struct {
	InputTokens  int
	OutputTokens int
	// CostUSD is set when the provider reports it (OpenRouter does).
	CostUSD *float64
}

// ClassifyResponse holds one Answer per question ID.
type ClassifyResponse struct {
	// Model is the versioned model that answered (e.g. "jev-1.13.0").
	Model   string
	Answers map[string]Answer
	Usage   Usage
}

// AIClassifierClient makes typed judgments (Jev). All judging in the
// pipeline goes through it.
type AIClassifierClient interface {
	Classify(ctx context.Context, req ClassifyRequest) (ClassifyResponse, error)
}

// ErrGenerativeUnavailable means no generative model is configured. Callers
// fall back to non-generated input.
var ErrGenerativeUnavailable = errors.New("generative client not configured")

// AIGenerativeClient writes text (an OpenAI-compatible chat API). In V1 it
// only writes the Job Summary.
type AIGenerativeClient interface {
	Generate(ctx context.Context, system, prompt string) (string, error)
}

// Metrics records pipeline measurements. The CLI implementation prints a
// run summary; a backend can plug in an OTel or Prometheus exporter.
type Metrics interface {
	Add(name string, delta int64)
	ObserveDuration(name string, d time.Duration)
}
