package app

import (
	"slices"
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
)

func texts(ss []domain.ContextSentence) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Text
	}
	return out
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "remote rocketship bullets and headers",
			in: "Software Engineer, Backend\n\nRole requirements:\n" +
				"• 4+ years of software engineering experience\n" +
				"• Bonus points for key-value stores (MongoDB, Memcache, Redis)\n" +
				"Tech stack:\nJavaScript, MongoDB, Go\n",
			want: []string{
				"Software Engineer, Backend",
				"Role requirements:",
				"4+ years of software engineering experience",
				"Bonus points for key-value stores (MongoDB, Memcache, Redis)",
				"Tech stack:",
				"JavaScript, MongoDB, Go",
			},
		},
		{
			name: "prose line with several sentences",
			in:   "5+ years of experience building backend systems. Solid proficiency in Go (or a systems language). You ship.",
			want: []string{
				"5+ years of experience building backend systems.",
				"Solid proficiency in Go (or a systems language).",
				"You ship.",
			},
		},
		{
			name: "abbreviations, versions and dotted names do not split",
			in:   "Experience with Node.js, e.g. NestJS, and U.S. export rules vs. EU ones. Python 3.12 is used, etc. Also Go.",
			want: []string{
				"Experience with Node.js, e.g. NestJS, and U.S. export rules vs. EU ones.",
				"Python 3.12 is used, etc. Also Go.",
			},
		},
		{
			name: "markdown headings, dash and numbered bullets",
			in:   "## What you'll bring\n- Kubernetes\n* Terraform\n1. Linux\n2) Bash",
			want: []string{"What you'll bring", "Kubernetes", "Terraform", "Linux", "Bash"},
		},
		{
			name: "blank and symbol-only lines are dropped, whitespace collapsed",
			in:   "  Go   and\tRust  \n\n•\n---\n",
			want: []string{"Go and Rust"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss, _ := splitSentences(tt.in)
			got := texts(ss)
			if !slices.Equal(got, tt.want) {
				t.Errorf("splitSentences()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestSplitSentencesAssignsRefs(t *testing.T) {
	got, _ := splitSentences("A.\nB. C.")
	want := []domain.Ref{"s1", "s2", "s3"}
	for i, s := range got {
		if s.Ref != want[i] {
			t.Errorf("sentence %d ref = %q, want %q", i, s.Ref, want[i])
		}
	}
}

func TestSplitSentencesHeadings(t *testing.T) {
	in := "Senior Engineer, Backend\n" +
		"About the role\n" +
		"We build payments.\n" +
		"Role requirements:\n" +
		"• 5+ years of Go\n" +
		"• Own web development\n" +
		"## Bonus points\n" +
		"- Kubernetes\n" +
		"Tech stack:\n" +
		"JavaScript, MongoDB, Go\n"
	_, got := splitSentences(in)
	want := []string{
		"",
		"About the role", "About the role",
		"Role requirements:", "Role requirements:", "Role requirements:",
		"Bonus points", "Bonus points",
		"Tech stack:", "Tech stack:",
	}
	if !slices.Equal(got, want) {
		t.Errorf("headings\n got: %q\nwant: %q", got, want)
	}
}
