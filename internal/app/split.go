package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

var (
	// listMarker matches a leading bullet, numbered item, or markdown heading.
	listMarker = regexp.MustCompile(`^(?:[•·▪◦●‣\-*]+|\d{1,3}[.)]|#{1,6})\s*`)
	whitespace = regexp.MustCompile(`\s+`)
)

// abbreviations end with a period but do not end a sentence. Tokens with an
// inner period ("e.g.", "U.S.") are handled separately.
var abbreviations = map[string]bool{
	"etc.": true, "vs.": true, "inc.": true, "ltd.": true, "co.": true,
	"corp.": true, "sr.": true, "jr.": true, "dr.": true, "mr.": true,
	"ms.": true, "mrs.": true, "no.": true, "approx.": true, "incl.": true,
	"dept.": true, "st.": true,
}

// splitSentences turns a Job Description into Context Sentences with Refs
// s1..sN. Each non-empty line is a sentence after list markers are stripped;
// prose lines are further split at sentence boundaries. Heading lines are
// kept as sentences too.
//
// headings[i] is the nearest heading at or above sentence i ("" if none).
// Section labeling passes it to Jev directly, because Jev cannot reliably
// locate a sentence by its position in a list.
func splitSentences(text string) (sentences []domain.ContextSentence, headings []string) {
	current := ""
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		marker := listMarker.FindString(line)
		line = strings.TrimSpace(whitespace.ReplaceAllString(line[len(marker):], " "))
		parts := splitLine(line)
		if isHeading(line, marker, len(parts)) {
			current = line
		}
		for _, p := range parts {
			if !hasAlnum(p) {
				continue
			}
			sentences = append(sentences, domain.ContextSentence{
				Ref:  domain.Ref(fmt.Sprintf("s%d", len(sentences)+1)),
				Text: p,
			})
			headings = append(headings, current)
		}
	}
	return sentences, headings
}

// isHeading reports whether a line (with its stripped list marker) reads as a
// section heading: a markdown heading, or a non-list line that ends with ":"
// or is short, comma-free, and unpunctuated ("About the role").
func isHeading(line, marker string, sentences int) bool {
	if sentences != 1 || !hasAlnum(line) {
		return false
	}
	if strings.HasPrefix(marker, "#") {
		return true
	}
	if marker != "" {
		return false
	}
	if strings.HasSuffix(line, ":") {
		return true
	}
	return len(strings.Fields(line)) <= 6 &&
		!strings.ContainsAny(line, ",") &&
		!strings.ContainsAny(line[len(line)-1:], ".!?)")
}

// splitLine splits one line at sentence-ending punctuation that is followed
// by a space and an uppercase letter, digit, or opening bracket/quote.
func splitLine(line string) []string {
	runes := []rune(line)
	var parts []string
	start := 0
	for i := 0; i < len(runes); i++ {
		if !strings.ContainsRune(".!?", runes[i]) {
			continue
		}
		// Include closing brackets/quotes that follow the punctuation.
		end := i + 1
		for end < len(runes) && strings.ContainsRune(`)]"'’”`, runes[end]) {
			end++
		}
		if end+1 >= len(runes) || runes[end] != ' ' || !startsSentence(runes[end+1]) {
			continue
		}
		if runes[i] == '.' && isAbbreviation(runes[start:i+1]) {
			continue
		}
		parts = append(parts, strings.TrimSpace(string(runes[start:end])))
		start = end + 1
		i = end
	}
	if rest := strings.TrimSpace(string(runes[start:])); rest != "" {
		parts = append(parts, rest)
	}
	return parts
}

func startsSentence(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsDigit(r) || strings.ContainsRune(`("'“‘[`, r)
}

// isAbbreviation reports whether the last token of s (which ends in '.') is
// a known abbreviation or contains an inner period, like "e.g." or "U.S.".
func isAbbreviation(s []rune) bool {
	tok := string(s)
	if i := strings.LastIndexAny(tok, " \t"); i >= 0 {
		tok = tok[i+1:]
	}
	tok = strings.TrimLeft(tok, `("'“‘[`)
	if abbreviations[strings.ToLower(tok)] {
		return true
	}
	return strings.Contains(strings.TrimSuffix(tok, "."), ".")
}

func hasAlnum(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}) >= 0
}

// splitSentencesStage is the pipeline stage wrapper.
func (e *Extractor) splitSentencesStage(_ context.Context, r *run) error {
	r.sentences, r.headings = splitSentences(r.jd.Text)
	if len(r.sentences) == 0 {
		return fmt.Errorf("job description has no sentences")
	}
	e.metrics.Add("sentences.total", int64(len(r.sentences)))
	return nil
}
