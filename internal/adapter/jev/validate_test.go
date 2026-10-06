package jev

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

func TestValidateAnswers(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	choice := port.Question{Type: port.Choice, Criteria: map[string]any{"a": "A", "b": nil}}
	score := port.Question{Type: port.Score, Criteria: []any{"low", "mid", "high"}}
	noul := port.Question{Type: port.Noul}
	for _, tt := range []struct {
		name string
		q    port.Question
		a    port.Answer
		want string // substring of the error; "" means valid
	}{
		{"noul ok", noul, port.Answer{Type: port.Noul, Noul: f(0.3)}, ""},
		{"noul rounding", noul, port.Answer{Type: port.Noul, Noul: f(1 + 1e-9)}, ""},
		{"noul missing", noul, port.Answer{Type: port.Noul}, "no noul value"},
		{"noul NaN", noul, port.Answer{Type: port.Noul, Noul: f(math.NaN())}, "outside [0, 1]"},
		{"noul above 1", noul, port.Answer{Type: port.Noul, Noul: f(1.2)}, "outside [0, 1]"},
		{"wrong type", noul, port.Answer{Type: port.Score, Score: f(1)}, `type "score", want "noul"`},
		{"score ok", score, port.Answer{Type: port.Score, Score: f(1.7)}, ""},
		{"score above top", score, port.Answer{Type: port.Score, Score: f(2.5)}, "outside [0, 2]"},
		{"score negative", score, port.Answer{Type: port.Score, Score: f(-0.5)}, "outside"},
		{"score infinite", score, port.Answer{Type: port.Score, Score: f(math.Inf(1))}, "outside"},
		{"choice ok", choice, port.Answer{Type: port.Choice, Choice: "a", Probabilities: map[string]float64{"a": 0.8, "b": 0.2}}, ""},
		{"choice unknown", choice, port.Answer{Type: port.Choice, Choice: "c", Probabilities: map[string]float64{"a": 1}}, `choice "c" is not an option`},
		{"choice empty distribution", choice, port.Answer{Type: port.Choice, Choice: "a"}, "empty distribution"},
		{"choice unknown probability", choice, port.Answer{Type: port.Choice, Choice: "a", Probabilities: map[string]float64{"a": 0.5, "z": 0.5}}, `unknown option "z"`},
		{"choice NaN probability", choice, port.Answer{Type: port.Choice, Choice: "a", Probabilities: map[string]float64{"a": math.NaN()}}, "outside [0, 1]"},
		{"confidence out of range", noul, port.Answer{Type: port.Noul, Noul: f(0.5), Confidence: f(-2)}, "confidence"},
	} {
		err := validateAnswers(map[string]port.Question{"q": tt.q}, map[string]port.Answer{"q": tt.a})
		if tt.want == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tt.name, err)
			}
			continue
		}
		var ae *AnswerError
		if !errors.As(err, &ae) || ae.Question != "q" || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want AnswerError containing %q", tt.name, err, tt.want)
		}
	}
}

func TestValidateAnswersMissing(t *testing.T) {
	err := validateAnswers(map[string]port.Question{"q": {Type: port.Noul}}, map[string]port.Answer{})
	var ae *AnswerError
	if !errors.As(err, &ae) || ae.Reason != "missing" {
		t.Errorf("error = %v, want missing AnswerError", err)
	}
}
