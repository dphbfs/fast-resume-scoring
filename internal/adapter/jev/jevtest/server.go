// Package jevtest provides a fake TypeSafe System One server for tests.
// go test never calls the live API.
package jevtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev"
)

// Model is the versioned model ID the fake server reports.
const Model = "jev-fake-1.0.0"

// Reply is what the fake server sends for one request: either an HTTP error
// (Status != 0) or Answers keyed by question ID.
type Reply struct {
	Status     int
	RetryAfter string
	Body       string
	Answers    map[string]jev.WireAnswer
}

// Responder produces the Reply for one decoded request. call is 0-based.
type Responder func(call int, req jev.WireRequest) Reply

// Server is a running fake that records every request it receives.
type Server struct {
	URL string

	mu       sync.Mutex
	requests []jev.WireRequest
}

// NewServer starts a fake server that answers with respond. It is closed
// when the test ends.
func NewServer(tb testing.TB, respond Responder) *Server {
	tb.Helper()
	s := &Server{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			http.NotFound(w, r)
			return
		}
		var req jev.WireRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		s.mu.Lock()
		call := len(s.requests)
		s.requests = append(s.requests, req)
		s.mu.Unlock()

		reply := respond(call, req)
		if reply.Status != 0 {
			if reply.RetryAfter != "" {
				w.Header().Set("Retry-After", reply.RetryAfter)
			}
			w.WriteHeader(reply.Status)
			_, _ = w.Write([]byte(reply.Body))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jev.WireResponse{
			Model:   Model,
			Answers: reply.Answers,
			Usage:   jev.WireUsage{InputTokens: 100, OutputTokens: 10 * len(reply.Answers)},
		})
	}))
	tb.Cleanup(ts.Close)
	s.URL = ts.URL
	return s
}

// Requests returns a copy of the requests received so far.
func (s *Server) Requests() []jev.WireRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]jev.WireRequest(nil), s.requests...)
}

// Noul builds a noul answer.
func Noul(p float64) jev.WireAnswer {
	return jev.WireAnswer{Type: "noul", Noul: &p}
}

// Choice builds a choice answer; the chosen option is the most probable one.
func Choice(probs map[string]float64, confidence float64) jev.WireAnswer {
	best, bestP := "", -1.0
	for opt, p := range probs {
		if p > bestP || (p == bestP && opt < best) {
			best, bestP = opt, p
		}
	}
	return jev.WireAnswer{Type: "choice", Choice: best, Probabilities: probs, Confidence: &confidence}
}

// Score builds a score answer.
func Score(score, confidence float64, probs map[string]float64) jev.WireAnswer {
	return jev.WireAnswer{Type: "score", Score: &score, Probabilities: probs, Confidence: &confidence}
}

// AnswerAll returns a Responder that answers every question with fn.
func AnswerAll(fn func(id string, q jev.WireQuestion) jev.WireAnswer) Responder {
	return func(_ int, req jev.WireRequest) Reply {
		answers := make(map[string]jev.WireAnswer, len(req.Questions))
		for id, q := range req.Questions {
			answers[id] = fn(id, q)
		}
		return Reply{Answers: answers}
	}
}
