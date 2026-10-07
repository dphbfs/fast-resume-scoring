package app

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

// TestSplitSentencesBulk runs the splitter over every Job Description in the
// gitignored bulk set (testdata/jd, see scripts/import_jds.py). It is skipped
// when the set is absent.
func TestSplitSentencesBulk(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/jd/*.txt")
	if len(files) == 0 {
		t.Skip("bulk set not present; run scripts/import_jds.py")
	}

	// Legal boilerplate reaches ~700 runes as one genuine sentence.
	const maxRunes = 800
	total, longest := 0, 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		ss, _ := splitSentences(string(raw))
		if len(ss) == 0 {
			t.Errorf("%s: no sentences", filepath.Base(f))
		}
		total += len(ss)
		for _, s := range ss {
			n := utf8.RuneCountInString(s.Text)
			longest = max(longest, n)
			if n > maxRunes {
				t.Errorf("%s %s: %d-rune sentence, likely a missed split: %.120q", filepath.Base(f), s.Ref, n, s.Text)
			}
		}
	}
	t.Logf("%d files, %d sentences (avg %.1f), longest %d runes",
		len(files), total, float64(total)/float64(len(files)), longest)
}
