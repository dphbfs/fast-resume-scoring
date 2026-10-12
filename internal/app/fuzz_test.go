package app

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

// Both parsers take untrusted text (a Job Description or Resume file), so
// they must never panic and must keep their output invariants on any input.

func FuzzParseResume(f *testing.F) {
	f.Add("# Work Experience\n## Engineer | Acme | 2021 – 2024\n- Built a Go service\n")
	f.Add("# Skills\nLanguages: Go, Python, Rust, Java, C, C++, Ruby, Kotlin (JVM, Android)\n")
	f.Add("no headings at all. Two sentences.\n\n* wrapped\n  bullet")
	f.Add("## | | |\n#\n- \n•\n")
	if b, err := os.ReadFile("../../examples/resume.md"); err == nil {
		f.Add(string(b))
	}
	f.Fuzz(func(t *testing.T, text string) {
		for _, u := range ParseResume(text) {
			checkUnitText(t, u)
		}
	})
}

func checkUnitText(t *testing.T, u domain.EvidenceUnit) {
	t.Helper()
	if u.Text != strings.TrimSpace(u.Text) || strings.ContainsAny(u.Text, "\r\n") {
		t.Fatalf("unit text not trimmed to one line: %q", u.Text)
	}
	if !hasAlnum(u.Text) {
		t.Fatalf("unit without letters or digits: %q", u.Text)
	}
}

func FuzzSplitSentences(f *testing.F) {
	f.Add("Requirements:\n- 5+ years of Go and Kubernetes.\n- AWS (e.g. EC2, S3) or GCP.\n")
	f.Add("# About us\nWe build things. Mr. Smith leads it! Do you?\n")
	f.Add("...\n-\n#\n:\n")
	if b, err := os.ReadFile("../../examples/job.md"); err == nil {
		f.Add(string(b))
	}
	f.Fuzz(func(t *testing.T, text string) {
		sentences, headings, strong := splitLines(text)
		if len(headings) != len(sentences) || len(strong) != len(sentences) {
			t.Fatalf("lengths differ: %d sentences, %d headings, %d strong",
				len(sentences), len(headings), len(strong))
		}
		for i, s := range sentences {
			if want := domain.Ref(fmt.Sprintf("s%d", i+1)); s.Ref != want {
				t.Fatalf("sentence %d has Ref %q, want %q", i, s.Ref, want)
			}
			if strings.ContainsAny(s.Text, "\r\n") || !hasAlnum(s.Text) {
				t.Fatalf("bad sentence text: %q", s.Text)
			}
		}
	})
}
