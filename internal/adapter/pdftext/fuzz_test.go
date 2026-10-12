package pdftext

import (
	"strings"
	"testing"
)

// normalize runs on text from an untrusted PDF: it must never panic, and its
// output has no blank lines and no non-breaking spaces.
func FuzzNormalize(f *testing.F) {
	f.Add("Experience\n•\nBuilt a Go service\n\n•Led a team\nSkills:\nGo, SQL")
	f.Add(" \n•\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		out := normalize(s)
		if strings.Contains(out, " ") {
			t.Fatalf("non-breaking space kept: %q", out)
		}
		for line := range strings.Lines(out) {
			if strings.TrimSpace(line) == "" {
				t.Fatalf("blank line in %q", out)
			}
		}
	})
}
