package app

import (
	"context"
	"strings"
	"unicode"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

// stopwords may appear inside a Candidate but never start or end one.
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "nor": true,
	"but": true, "of": true, "in": true, "on": true, "at": true, "to": true,
	"for": true, "with": true, "without": true, "by": true, "from": true,
	"as": true, "into": true, "onto": true, "over": true, "under": true,
	"within": true, "across": true, "via": true, "per": true, "than": true,
	"is": true, "are": true, "be": true, "been": true, "being": true,
	"was": true, "were": true, "has": true, "have": true, "having": true,
	"do": true, "does": true, "our": true, "your": true, "their": true,
	"its": true, "you": true, "we": true, "they": true, "it": true,
	"this": true, "that": true, "these": true, "those": true, "who": true,
	"which": true, "what": true, "where": true, "when": true, "how": true,
	"will": true, "can": true, "should": true, "must": true, "may": true,
	"not": true, "no": true, "such": true, "like": true, "including": true,
	"e.g": true, "i.e": true, "etc": true, "also": true, "other": true,
	"any": true, "all": true, "both": true, "either": true, "more": true,
	"most": true, "very": true, "least": true, "some": true,
}

// defaultMaxWindowWords mirrors the PIPELINE_MAX_WINDOW_WORDS default; tests
// use it to measure coverage at the shipped setting.
const defaultMaxWindowWords = 4

const (
	openers = `("'“‘[{`
	closers = `,;:.!?"')]}’”`
	// breakers end a clause: a window may not continue past them.
	breakers = `,;:.!?)]}`
)

// token is one word of a sentence with the punctuation around it removed.
type token struct {
	text string
	// breakAfter is true when a clause boundary (comma, bracket, sentence
	// punctuation, dash) follows the token.
	breakAfter bool
}

// dashes separate clauses even when glued to words ("DeFi—whether").
var dashes = strings.NewReplacer("—", " — ", "–", " – ")

// tokenize splits a sentence into tokens, keeping technical names such as
// "Node.js", "C++", "C#", "CI/CD" and "X.509" intact. With splitSlash,
// slash-joined names whose parts all have 3+ runes ("TypeScript/Node.js")
// become separate tokens with a break between them, since they usually list
// alternatives; without it they stay whole ("client/server").
func tokenize(sentence string, splitSlash bool) []token {
	var toks []token
	markBreak := func() {
		if len(toks) > 0 {
			toks[len(toks)-1].breakAfter = true
		}
	}
	for _, raw := range strings.Fields(dashes.Replace(sentence)) {
		lead := raw[:len(raw)-len(strings.TrimLeft(raw, openers))]
		text := strings.TrimLeft(raw, openers)
		trimmed := strings.TrimRight(text, closers)
		trail := text[len(trimmed):]

		if strings.ContainsAny(lead, "([{") || !hasAlnum(trimmed) {
			markBreak()
		}
		if !hasAlnum(trimmed) {
			continue
		}
		parts := []string{trimmed}
		if splitSlash {
			parts = splitSlashes(trimmed)
		}
		for i, p := range parts {
			toks = append(toks, token{text: p, breakAfter: i < len(parts)-1})
		}
		toks[len(toks)-1].breakAfter = strings.ContainsAny(trail, breakers)
	}
	return toks
}

// splitSlashes splits "a/b" when every part has at least 3 runes, so short
// pairs like "CI/CD" or "A/B" stay whole.
func splitSlashes(tok string) []string {
	parts := strings.Split(tok, "/")
	if len(parts) == 1 {
		return parts
	}
	for _, p := range parts {
		if len([]rune(p)) < 3 || !hasAlnum(p) {
			return []string{tok}
		}
	}
	return parts
}

// sentenceCandidates returns every pruned window of 1..maxWords tokens:
// windows never start or end on a stopword, never cross a clause break,
// contain a letter, and are unique (case-insensitively) within the sentence.
// Windows come from both tokenizations (slash-joined names split and whole),
// because text alone cannot tell "Terraform/Terragrunt" (two tools) from
// "client/server" (one concept).
func sentenceCandidates(s domain.ContextSentence, maxWords int) []domain.Candidate {
	seen := map[string]bool{}
	var out []domain.Candidate
	for _, splitSlash := range []bool{true, false} {
		toks := tokenize(s.Text, splitSlash)
		for start := range toks {
			if isStopword(toks[start].text) {
				continue
			}
			for end := start + 1; end <= min(start+maxWords, len(toks)); end++ {
				last := toks[end-1]
				if !isStopword(last.text) {
					words := make([]string, 0, end-start)
					for _, t := range toks[start:end] {
						words = append(words, t.text)
					}
					text := strings.Join(words, " ")
					key := strings.ToLower(text)
					if hasLetter(text) && !seen[key] {
						seen[key] = true
						out = append(out, domain.Candidate{Text: text, Ref: s.Ref})
					}
				}
				if last.breakAfter {
					break
				}
			}
		}
	}
	return out
}

func isStopword(w string) bool { return stopwords[strings.ToLower(w)] }

func hasLetter(s string) bool { return strings.IndexFunc(s, unicode.IsLetter) >= 0 }

// generateCandidates builds Candidates from every sentence whose Section is
// not dropped.
func (e *Extractor) generateCandidates(ctx context.Context, r *run) error {
	r.candidates = r.candidates[:0]
	for _, s := range r.sentences {
		if droppedSections[s.Section] {
			continue
		}
		cs := sentenceCandidates(s, e.cfg.MaxWindowWords)
		e.log.DebugContext(ctx, "candidates", "ref", s.Ref, "count", len(cs))
		r.candidates = append(r.candidates, cs...)
	}
	e.metrics.Add("candidates.total", int64(len(r.candidates)))
	return nil
}
