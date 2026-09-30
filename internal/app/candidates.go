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

// genericWords carry no requirement on their own. A Candidate made only of
// generic words and stopwords ("Hands-on experience", "Bonus") is dropped.
var genericWords = map[string]bool{
	"experience": true, "experiences": true, "skill": true, "skills": true,
	"ability": true, "abilities": true, "knowledge": true, "background": true,
	"expertise": true, "familiarity": true, "proficiency": true,
	"understanding": true, "bonus": true, "plus": true, "points": true,
	"requirement": true, "requirements": true, "qualifications": true,
	"hands-on": true, "strong": true, "solid": true, "deep": true,
	"proven": true, "demonstrated": true, "excellent": true, "good": true,
	"great": true, "working": true, "professional": true, "relevant": true,
	"related": true, "similar": true, "equivalent": true, "track": true,
	"record": true, "comfort": true, "passion": true, "interest": true,
}

// isGenericOnly reports whether every word of s is generic or a stopword.
func isGenericOnly(s string) bool {
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if !genericWords[w] && !stopwords[w] {
			return false
		}
	}
	return true
}

// separators split a sentence into chunks and are left out of them. "such"
// is only a separator when followed by "as".
var separators = map[string]bool{
	"and": true, "or": true, "nor": true, "&": true, "+": true, "plus": true,
	"with": true, "including": true, "like": true, "e.g": true, "i.e": true,
	"especially": true, "preferably": true, "ideally": true, "such": true,
}

// maxWholeChunkWords is the longest chunk that is itself offered as an
// option, so long qualifiers ("5+ years of software engineering experience")
// can be selected whole.
const maxWholeChunkWords = 8

// maxChunkOptions keeps each Choice within Jev's 255-option limit, leaving
// room for the rejectOptions.
const maxChunkOptions = 250

// chunk is a clause-sized piece of a sentence, cut at clause breaks and list
// words, that names at most one Requirement. Options are its Candidates.
type chunk struct {
	Ref     domain.Ref
	Text    string
	Options []string
}

// chunkSentence cuts a sentence into chunks at clause breaks (commas,
// brackets, dashes, sentence punctuation) and list words ("and", "or",
// "with", "such as", "including", "e.g."). Chunks with only stopwords are
// dropped.
func chunkSentence(s domain.ContextSentence, maxWords int) []chunk {
	toks := tokenize(s.Text, false)
	var out []chunk
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		text := strings.Join(cur, " ")
		cur = cur[:0]
		if c, ok := newChunk(s.Ref, text, maxWords); ok {
			out = append(out, c)
		}
	}
	for i, t := range toks {
		w := strings.ToLower(t.text)
		isSep := separators[w] && (w != "such" || (i+1 < len(toks) && strings.EqualFold(toks[i+1].text, "as")))
		if isSep || (w == "as" && i > 0 && strings.EqualFold(toks[i-1].text, "such")) {
			flush()
			continue
		}
		cur = append(cur, t.text)
		if t.breakAfter {
			flush()
		}
	}
	flush()
	return out
}

// newChunk builds a chunk whose options are its pruned windows (both slash
// tokenizations) plus the whole chunk when it is short enough.
func newChunk(ref domain.Ref, text string, maxWords int) (chunk, bool) {
	var opts []string
	seen := map[string]bool{}
	add := func(o string) {
		if isGenericOnly(o) {
			return
		}
		if k := strings.ToLower(o); !seen[k] && len(opts) < maxChunkOptions {
			seen[k] = true
			opts = append(opts, o)
		}
	}
	words := strings.Fields(text)
	if len(words) <= maxWholeChunkWords && !isStopword(words[0]) && !isStopword(words[len(words)-1]) && hasLetter(text) {
		add(text)
	}
	for _, c := range sentenceCandidates(domain.ContextSentence{Ref: ref, Text: text}, maxWords) {
		add(c.Text)
	}
	return chunk{Ref: ref, Text: text, Options: opts}, len(opts) > 0
}

// generateCandidates builds the chunks, and their Candidate options, of every
// sentence whose Section is not dropped. Heading lines ("Bonus Points") are
// skipped: they label the sentences below them and name no Requirement.
func (e *Extractor) generateCandidates(ctx context.Context, r *run) error {
	r.chunks = r.chunks[:0]
	r.trace.Sentences = make([]domain.TraceSentence, len(r.sentences))
	options := 0
	for i, s := range r.sentences {
		dropped := droppedSections[s.Section] || (i < len(r.headings) && r.headings[i] == s.Text)
		ts := domain.TraceSentence{Ref: s.Ref, Text: s.Text, Section: s.Section, Dropped: dropped}
		if i < len(r.headings) {
			ts.Heading = r.headings[i]
		}
		if i < len(r.sectionConf) {
			ts.Confidence = r.sectionConf[i]
		}
		r.trace.Sentences[i] = ts
		if dropped {
			continue
		}
		cs := chunkSentence(s, e.cfg.MaxWindowWords)
		for _, c := range cs {
			options += len(c.Options)
		}
		e.log.DebugContext(ctx, "chunks", "ref", s.Ref, "count", len(cs))
		r.chunks = append(r.chunks, cs...)
	}
	e.metrics.Add("chunks.total", int64(len(r.chunks)))
	e.metrics.Add("candidates.total", int64(options))
	return nil
}
