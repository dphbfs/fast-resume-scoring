package jev

import (
	"fmt"
	"math"

	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// probEpsilon tolerates rounding in probabilities and distribution sums.
const probEpsilon = 1e-6

// AnswerError is an answer that does not fit its question: wrong type, a
// value outside its range, or a choice that is not one of the options.
// Retrying does not help; the caller decides whether the run can go on.
type AnswerError struct {
	Question string
	Reason   string
}

func (e *AnswerError) Error() string {
	return fmt.Sprintf("jev: invalid answer for question %q: %s", e.Question, e.Reason)
}

// validateAnswers checks every question has an answer that fits it.
func validateAnswers(questions map[string]port.Question, answers map[string]port.Answer) error {
	for id, q := range questions {
		a, ok := answers[id]
		if !ok {
			return &AnswerError{Question: id, Reason: "missing"}
		}
		if reason := checkAnswer(q, a); reason != "" {
			return &AnswerError{Question: id, Reason: reason}
		}
	}
	return nil
}

// checkAnswer returns why a does not fit q, or "" when it does.
func checkAnswer(q port.Question, a port.Answer) string {
	if a.Type != q.Type {
		return fmt.Sprintf("type %q, want %q", a.Type, q.Type)
	}
	if a.Confidence != nil && !isProb(*a.Confidence) {
		return fmt.Sprintf("confidence %v outside [0, 1]", *a.Confidence)
	}
	for opt, p := range a.Probabilities {
		if !isProb(p) {
			return fmt.Sprintf("probability %v for %q outside [0, 1]", p, opt)
		}
	}
	switch q.Type {
	case port.Noul:
		if a.Noul == nil {
			return "no noul value"
		}
		if !isProb(*a.Noul) {
			return fmt.Sprintf("noul %v outside [0, 1]", *a.Noul)
		}
	case port.Score:
		if a.Score == nil {
			return "no score value"
		}
		top := math.Inf(1)
		if levels, ok := q.Criteria.([]any); ok {
			top = float64(len(levels) - 1)
		}
		if s := *a.Score; math.IsNaN(s) || s < -probEpsilon || s > top+probEpsilon {
			return fmt.Sprintf("score %v outside [0, %v]", s, top)
		}
	case port.Choice:
		if a.Choice == "" {
			return "no choice"
		}
		if len(a.Probabilities) == 0 {
			return "empty distribution"
		}
		options, ok := q.Criteria.(map[string]any)
		if !ok {
			break
		}
		if _, ok := options[a.Choice]; !ok {
			return fmt.Sprintf("choice %q is not an option", a.Choice)
		}
		for opt := range a.Probabilities {
			if _, ok := options[opt]; !ok {
				return fmt.Sprintf("probability for unknown option %q", opt)
			}
		}
	}
	return ""
}

func isProb(p float64) bool {
	return !math.IsNaN(p) && p >= -probEpsilon && p <= 1+probEpsilon
}
