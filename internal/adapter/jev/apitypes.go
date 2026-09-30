package jev

import "github.com/dphbfs/fast-resume-tailoring/internal/port"

// Wire types mirror the HTTP API (https://docs.typesafe.ai/api).
// They are exported so jevtest can decode requests and encode responses.

// WireRequest is the POST /v1/systemone body.
type WireRequest struct {
	State     any                     `json:"state"`
	Model     string                  `json:"model"`
	Questions map[string]WireQuestion `json:"questions"`
}

// WireQuestion is one typed question.
type WireQuestion struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// WireAnswer is one typed answer; fields present depend on Type.
type WireAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
}

// WireUsage is the token usage of one call.
type WireUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// WireResponse is the POST /v1/systemone response body.
type WireResponse struct {
	Model   string                `json:"model"`
	Answers map[string]WireAnswer `json:"answers"`
	Usage   WireUsage             `json:"usage"`
}

func toWire(model string, req port.ClassifyRequest) WireRequest {
	qs := make(map[string]WireQuestion, len(req.Questions))
	for id, q := range req.Questions {
		qs[id] = WireQuestion{Type: string(q.Type), Instructions: q.Instructions, Criteria: q.Criteria}
	}
	return WireRequest{State: req.State, Model: model, Questions: qs}
}

func fromWire(w WireResponse) port.ClassifyResponse {
	answers := make(map[string]port.Answer, len(w.Answers))
	for id, a := range w.Answers {
		answers[id] = port.Answer{
			Type:          port.QuestionType(a.Type),
			Noul:          a.Noul,
			Choice:        a.Choice,
			Score:         a.Score,
			Probabilities: a.Probabilities,
			Confidence:    a.Confidence,
		}
	}
	return port.ClassifyResponse{
		Model:   w.Model,
		Answers: answers,
		Usage:   port.Usage{InputTokens: w.Usage.InputTokens, OutputTokens: w.Usage.OutputTokens},
	}
}
