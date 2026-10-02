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
//   - in skills, each line is a unit; a line listing more than
//     skillsChunkItems comma-separated items is cut into balanced chunks,
//     each repeating the line's label ("Stack: a, b" ...).
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
			p.emitLine(line)
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
		p.emitLine(p.bullet)
		p.bullet, p.inBullet = "", false
	}
	if len(p.paragraph) > 0 {
		for _, s := range splitLine(strings.Join(p.paragraph, " ")) {
			p.emit(s)
		}
		p.paragraph = nil
	}
}

// emitLine emits a bullet or line: skills lines may become several units.
func (p *resumeParser) emitLine(text string) {
	if p.section != domain.ResumeSkills {
		p.emit(text)
		return
	}
	for _, chunk := range splitSkillsLine(text) {
		p.emit(chunk)
	}
}

// skillsChunkItems is the most items one skills unit lists. The Retrieval
// Round keeps at most CHECKER_RETRIEVAL_K (8) Requirements per unit, so a
// 26-item skills line could support only 8 Requirements; 6 leaves room for
// an item that names two Requirements.
const skillsChunkItems = 6

// splitSkillsLine cuts a skills line with more than skillsChunkItems
// comma-separated items (commas inside brackets don't count) into balanced
// chunks, each prefixed with the line's label: the text before a colon that
// comes before the first comma and has at most 4 words. Shorter lines come
// back unchanged.
func splitSkillsLine(line string) []string {
	label, rest := "", line
	if i := strings.Index(line, ":"); i > 0 && !strings.Contains(line[:i], ",") && len(strings.Fields(line[:i])) <= 4 {
		label, rest = line[:i+1]+" ", line[i+1:]
	}
	var items []string
	depth, start := 0, 0
	for i, r := range rest {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			depth = max(depth-1, 0)
		case ',':
			if depth == 0 {
				items = append(items, strings.TrimSpace(rest[start:i]))
				start = i + 1
			}
		}
	}
	items = append(items, strings.TrimSpace(rest[start:]))
	if len(items) <= skillsChunkItems {
		return []string{line}
	}
	n := (len(items) + skillsChunkItems - 1) / skillsChunkItems
	out := make([]string, 0, n)
	for i := range n {
		lo, hi := (i*len(items)+n-1)/n, ((i+1)*len(items)+n-1)/n
		out = append(out, label+strings.Join(items[lo:hi], ", "))
	}
	return out
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
