package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

func TestParseResume(t *testing.T) {
	text := `Jane Doe
jane@example.com | +1 555 0100

# Summary
Backend engineer with 8 years in payments. Loves Go.

# Work Experience
## Senior Backend Engineer | Acme Pay | 2021-03 – 2024-06
- Migrated a monolith to Go microservices
  behind feature flags.
• Ran services on EKS.
* Mentored 3 engineers.

## Software Engineer | Globex
Built billing APIs in Python.

# Technical Skills
Languages: Go, Python, SQL
- Cloud: AWS, Kubernetes

# Education & Certifications
## BSc Computer Science | Fake University | 2012 – 2016
- AWS Certified Solutions Architect
`
	want := []domain.EvidenceUnit{
		{ID: "e1", Text: "Backend engineer with 8 years in payments.", ResumeSection: domain.ResumeSummary},
		{ID: "e2", Text: "Loves Go.", ResumeSection: domain.ResumeSummary},
		{ID: "e3", Text: "Migrated a monolith to Go microservices behind feature flags.", ResumeSection: domain.ResumeExperience,
			Role: "Senior Backend Engineer", Company: "Acme Pay", Dates: "2021-03 – 2024-06"},
		{ID: "e4", Text: "Ran services on EKS.", ResumeSection: domain.ResumeExperience,
			Role: "Senior Backend Engineer", Company: "Acme Pay", Dates: "2021-03 – 2024-06"},
		{ID: "e5", Text: "Mentored 3 engineers.", ResumeSection: domain.ResumeExperience,
			Role: "Senior Backend Engineer", Company: "Acme Pay", Dates: "2021-03 – 2024-06"},
		{ID: "e6", Text: "Built billing APIs in Python.", ResumeSection: domain.ResumeExperience,
			Role: "Software Engineer", Company: "Globex"},
		{ID: "e7", Text: "Languages: Go, Python, SQL", ResumeSection: domain.ResumeSkills},
		{ID: "e8", Text: "Cloud: AWS, Kubernetes", ResumeSection: domain.ResumeSkills},
		{ID: "e9", Text: "BSc Computer Science, Fake University", ResumeSection: domain.ResumeEducation,
			Role: "BSc Computer Science", Company: "Fake University", Dates: "2012 – 2016"},
		{ID: "e10", Text: "AWS Certified Solutions Architect", ResumeSection: domain.ResumeEducation,
			Role: "BSc Computer Science", Company: "Fake University", Dates: "2012 – 2016"},
	}
	assertUnits(t, ParseResume(text), want)
}

func TestParseResumeSkillsLinesAreNotSentenceSplit(t *testing.T) {
	got := ParseResume("# Skills\nGo. Python. SQL\n- Cloud: AWS\nDatabases: Postgres\n")
	assertUnits(t, got, []domain.EvidenceUnit{
		{ID: "e1", Text: "Go. Python. SQL", ResumeSection: domain.ResumeSkills},
		{ID: "e2", Text: "Cloud: AWS", ResumeSection: domain.ResumeSkills},
		{ID: "e3", Text: "Databases: Postgres", ResumeSection: domain.ResumeSkills},
	})
}

func TestParseResumeWithoutHeadings(t *testing.T) {
	got := ParseResume("- Built a Kafka pipeline.\n- Wrote Terraform modules.\n")
	assertUnits(t, got, []domain.EvidenceUnit{
		{ID: "e1", Text: "Built a Kafka pipeline.", ResumeSection: domain.ResumeOther},
		{ID: "e2", Text: "Wrote Terraform modules.", ResumeSection: domain.ResumeOther},
	})
}

func TestResumeSectionOf(t *testing.T) {
	tests := map[string]domain.ResumeSection{
		"Professional Experience":   domain.ResumeExperience,
		"Employment History":        domain.ResumeExperience,
		"Side Projects":             domain.ResumeProjects,
		"Project Experience":        domain.ResumeProjects,
		"Technical Skills":          domain.ResumeSkills,
		"Tech Stack":                domain.ResumeSkills,
		"Profile":                   domain.ResumeSummary,
		"About Me":                  domain.ResumeSummary,
		"Education":                 domain.ResumeEducation,
		"Licenses & Certifications": domain.ResumeCertifications,
		"Languages and Interests":   domain.ResumeOther,
	}
	for heading, want := range tests {
		if got := resumeSectionOf(heading); got != want {
			t.Errorf("resumeSectionOf(%q) = %q, want %q", heading, got, want)
		}
	}
}

func assertUnits(t *testing.T, got, want []domain.EvidenceUnit) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d units, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unit %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

// TestParseResumeFixtures checks every committed Resume fixture parses into
// units with metadata and skips the name/contact preamble.
func TestParseResumeFixtures(t *testing.T) {
	paths, err := filepath.Glob("../../testdata/resumes/*.md")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no resume fixtures: %v", err)
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			units := ParseResume(string(raw))
			if len(units) < 10 {
				t.Fatalf("only %d units", len(units))
			}
			sections := map[domain.ResumeSection]int{}
			for _, u := range units {
				sections[u.ResumeSection]++
				if u.ResumeSection == domain.ResumeExperience && (u.Role == "" || u.Company == "" || u.Dates == "") {
					t.Errorf("experience unit without metadata: %+v", u)
				}
			}
			if sections[domain.ResumeExperience] == 0 || sections[domain.ResumeSkills] == 0 {
				t.Errorf("sections = %v, want experience and skills", sections)
			}
			if first := units[0].ResumeSection; first == domain.ResumeOther {
				t.Errorf("first unit %q is outside any section (preamble not skipped)", units[0].Text)
			}
		})
	}
}
