package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/openai"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
)

// BaselineConfig turns on the generative baseline arm of the checker eval
// and prices its tokens. Prices are USD per million tokens; they are only
// used when the provider does not report the cost itself.
type BaselineConfig struct {
	Enabled      bool    `json:"enabled"`
	Model        string  `json:"model"`
	PriceInPerM  float64 `json:"price_in_per_m"`
	PriceOutPerM float64 `json:"price_out_per_m"`
}

// Completer is the generative call the baseline makes (*openai.Client).
type Completer interface {
	Complete(ctx context.Context, system, prompt string) (openai.Reply, error)
}

// Baseline scores a Resume against a Job Description the traditional way:
// one generative prompt (the generative model approach's match score).
// It is the arm the Jev pipeline is compared against on accuracy, cost
// and latency.
type Baseline struct {
	gen Completer
	cfg BaselineConfig
}

// NewBaseline returns nil when cfg is not enabled; the runner then skips
// the baseline arm. The model name is taken from gcfg for the report.
func NewBaseline(gen *openai.Client, gcfg config.Generative, cfg BaselineConfig) *Baseline {
	if !cfg.Enabled {
		return nil
	}
	cfg.Model = gcfg.Model
	return &Baseline{gen: gen, cfg: cfg}
}

// BaselineScore is the baseline's answer for one pair.
type BaselineScore struct {
	Score        *int     `json:"score"`
	Gaps         []string `json:"gaps"`
	Strengths    []string `json:"strengths"`
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	// InputTokensEstimated is set when the provider's input count was
	// implausible and InputTokens is estimated from the prompt length.
	InputTokensEstimated bool `json:"input_tokens_estimated,omitempty"`
	// CostUSD is the provider-reported cost, or tokens times the configured
	// prices; nil when neither is known.
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// CostEstimated is set when CostUSD comes from the configured prices,
	// so a rescore can reprice it.
	CostEstimated bool   `json:"cost_estimated,omitempty"`
	DurationMS    int64  `json:"duration_ms"`
	Error         string `json:"error,omitempty"`
}

// baselinePrompt is the generative model approach's match-score prompt,
// with the Resume passed as text instead of JSON.
const baselinePrompt = "Compare this resume against the job description. Return ONLY JSON with keys score (integer 0-100 fit), gaps (array of short missing-qualification strings), strengths (array of short matching-strength strings).\n\nRESUME:\n%s\n\nJOB DESCRIPTION:\n%s"

const (
	baselineMaxItems = 8
	// charsPerToken is a rough English average, for estimates only.
	charsPerToken = 4
)

// Score asks the generative model for one pair's fit. A failed call or an
// unparsable reply is returned in BaselineScore.Error, never as an error,
// so the Jev arm of the same fixture still counts.
func (b *Baseline) Score(ctx context.Context, jd, resume string) BaselineScore {
	prompt := fmt.Sprintf(baselinePrompt, resume, jd)
	reply, err := b.gen.Complete(ctx, "", prompt)
	s := BaselineScore{
		InputTokens:  reply.Usage.InputTokens,
		OutputTokens: reply.Usage.OutputTokens,
		DurationMS:   reply.Latency.Milliseconds(),
		CostUSD:      reply.Usage.Cost,
	}
	// Some OpenAI-compatible proxies report a token or two for any prompt.
	// Below a quarter of the length estimate, use the estimate instead.
	if est := len(prompt) / charsPerToken; s.InputTokens < est/4 {
		s.InputTokens, s.InputTokensEstimated = est, true
	}
	if s.CostUSD == nil {
		s.price(b.cfg)
	}
	if err != nil {
		s.Error = err.Error()
		return s
	}
	score, gaps, strengths, err := parseBaselineReply(reply.Text)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	s.Score, s.Gaps, s.Strengths = &score, gaps, strengths
	return s
}

// price sets the cost from the configured prices; without prices it
// clears it.
func (s *BaselineScore) price(cfg BaselineConfig) {
	s.CostUSD, s.CostEstimated = nil, false
	if cfg.PriceInPerM <= 0 && cfg.PriceOutPerM <= 0 {
		return
	}
	c := (float64(s.InputTokens)*cfg.PriceInPerM + float64(s.OutputTokens)*cfg.PriceOutPerM) / 1e6
	s.CostUSD, s.CostEstimated = &c, true
}

// RepriceBaseline reprices, with cfg's prices, every baseline call of rep
// whose cost the provider did not report. Totals are not recomputed; run
// RescoreChecker after it.
func RepriceBaseline(rep *CheckerReport, cfg BaselineConfig) {
	if rep.Baseline == nil {
		return
	}
	rep.Baseline.PriceInPerM, rep.Baseline.PriceOutPerM = cfg.PriceInPerM, cfg.PriceOutPerM
	for i := range rep.Fixtures {
		if b := rep.Fixtures[i].Baseline; b != nil && (b.CostUSD == nil || b.CostEstimated) {
			b.price(*rep.Baseline)
		}
	}
}

// parseBaselineReply reads the reply as tolerantly as the generative
// model approach does: code fences and surrounding prose are ignored, the
// score is rounded and clamped to 0-100, and each list is capped at 8
// items.
func parseBaselineReply(text string) (int, []string, []string, error) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return 0, nil, nil, fmt.Errorf("baseline: no JSON object in reply %q", text)
	}
	var out struct {
		Score     json.Number `json:"score"`
		Gaps      []string    `json:"gaps"`
		Strengths []string    `json:"strengths"`
	}
	dec := json.NewDecoder(strings.NewReader(text[start : end+1]))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return 0, nil, nil, fmt.Errorf("baseline: decode reply: %w", err)
	}
	f, err := out.Score.Float64()
	if err != nil {
		return 0, nil, nil, fmt.Errorf("baseline: score %q: %w", out.Score, err)
	}
	score := int(math.Max(0, math.Min(100, math.Round(f))))
	return score, capList(out.Gaps), capList(out.Strengths), nil
}

func capList(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items[:min(len(items), baselineMaxItems)]
}
