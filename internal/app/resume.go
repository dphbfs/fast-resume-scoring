package app

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

var (
	// resumeBullet matches a leading bullet glyph followed by whitespace, so
	// "**bold**" is not taken for a bullet.
	resumeBullet = regexp.MustCompile(`^[-*•·▪◦●‣]\s+`)
	// sectionHeading is "# Heading"; entryHeading is "## Role | Company | dates".
	sectionHeading = regexp.MustCompile(`^#\s+(.*)$`)
	entryHeading   = regexp.MustCompile(`^#{2,6}\s+(.*)$`)
	anySection     = regexp.MustCompile(`(?m)^\s*#\s`)
)

// resumeSectionKeywords maps heading words to Resume Sections, checked in
// order so "Project Experience" is projects and "Education &
// Certifications" is education.
var resumeSectionKeywords = []struct {
	section  domain.ResumeSection
	keywords []string
}{
	{domain.ResumeProjects, []string{"project"}},
	{domain.ResumeEducation, []string{"education"}},
	{domain.ResumeCertifications, []string{"certif", "licens"}},
	{domain.ResumeSkills, []string{"skill", "technolog", "tech stack", "tools"}},
	{domain.ResumeSummary, []string{"summary", "profile", "about", "objective"}},
	{domain.ResumeExperience, []string{"experience", "employment", "work history", "career"}},
}

// resumeSectionOf maps a "# Heading" to its Resume Section.
func resumeSectionOf(heading string) domain.ResumeSection {
	h := strings.ToLower(heading)
	for _, k := range resumeSectionKeywords {
		for _, w := range k.keywords {
			if strings.Contains(h, w) {
				return k.section
			}
		}
	}
	return domain.ResumeOther
}

// ParseResume turns a Resume in the markdown convention (see CLAUDE.md)
// into Evidence Units e1..eN, kept verbatim:
//
//   - "# Heading" starts a Resume Section; text above the first one (name,
//     contacts) is skipped. A Resume with no such heading is one "other"
//     section.
//   - "## Role | Company | dates" sets the metadata of the units below it;
//     in education and certifications it is a unit itself.
//   - in skills, each line is a unit.
//   - elsewhere each bullet is a unit (lines directly under it continue it)
//     and prose paragraphs are split into sentences.
func ParseResume(text string) []domain.EvidenceUnit {
	p := resumeParser{section: domain.ResumeOther}
	skipping := anySection.MatchString(text)
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if m := sectionHeading.FindStringSubmatch(line); m != nil {
			skipping = false
			p.flush()
			p.section, p.entry = resumeSectionOf(m[1]), domain.EvidenceUnit{}
			continue
		}
		if skipping {
			continue
		}
		switch {
		case line == "":
			p.flush()
		case entryHeading.MatchString(line):
			p.flush()
			p.startEntry(entryHeading.FindStringSubmatch(line)[1])
		case resumeBullet.MatchString(line):
			p.flush()
			p.bullet = resumeBullet.ReplaceAllString(line, "")
			p.inBullet = true
		case p.section == domain.ResumeSkills:
			p.flush()
			p.emit(line)
		case p.inBullet:
			p.bullet += " " + line
		default:
			p.paragraph = append(p.paragraph, line)
		}
	}
	p.flush()
	for i := range p.units {
		p.units[i].ID = fmt.Sprintf("e%d", i+1)
	}
	return p.units
}

type resumeParser struct {
	section domain.ResumeSection
	// entry holds the Role, Company, and Dates of the current entry header.
	entry     domain.EvidenceUnit
	bullet    string
	inBullet  bool
	paragraph []string
	units     []domain.EvidenceUnit
}

func (p *resumeParser) startEntry(header string) {
	parts := strings.Split(header, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	parts = append(parts, "", "", "")
	p.entry = domain.EvidenceUnit{Role: parts[0], Company: parts[1], Dates: parts[2]}
	if p.section == domain.ResumeEducation || p.section == domain.ResumeCertifications {
		var named []string
		for _, s := range parts[:2] {
			if s != "" {
				named = append(named, s)
			}
		}
		p.emit(strings.Join(named, ", "))
	}
}

// flush emits the open bullet or prose paragraph, if any.
func (p *resumeParser) flush() {
	if p.inBullet {
		p.emit(p.bullet)
		p.bullet, p.inBullet = "", false
	}
	if len(p.paragraph) > 0 {
		for _, s := range splitLine(strings.Join(p.paragraph, " ")) {
			p.emit(s)
		}
		p.paragraph = nil
	}
}

func (p *resumeParser) emit(text string) {
	text = strings.TrimSpace(whitespace.ReplaceAllString(text, " "))
	if !hasAlnum(text) {
		return
	}
	u := p.entry
	u.Text, u.ResumeSection = text, p.section
	p.units = append(p.units, u)
}
