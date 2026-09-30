package app

import (
	"fmt"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// This file holds every Jev question and the policy constants applied to the
// answers, so they can be reviewed and tuned in one place.

// --- Section labeling (stage 2) ---

// sectionCriteria describes each Section option for the labeling Choice.
var sectionCriteria = map[string]any{
	string(domain.SectionRequired): "A qualification the candidate must have: required skills, " +
		"experience, education, or minimum qualifications.",
	string(domain.SectionPreferred): "A qualification that is optional: preferred, bonus, " +
		"nice-to-have, \"a plus\", or ideal-candidate extras.",
	string(domain.SectionResponsibilities): "What the person will do in the role: duties, " +
		"projects, ownership, and day-to-day work.",
	string(domain.SectionCompany): "About the company, team, product, mission, or culture " +
		"rather than about the candidate or the work.",
	string(domain.SectionBenefits): "Salary, equity, bonus, insurance, time off, perks, or " +
		"other compensation and benefits.",
	string(domain.SectionOther): "A heading, job title, legal or equal-opportunity text, " +
		"application instructions, location or logistics, or anything else.",
}

// sectionQuestion asks which Section one sentence belongs to. The sentence
// and its nearest heading are embedded in the question itself: Jev cannot
// reliably find a sentence by its position in a list (live test, jev-1.13
// labeled neighbouring sentences).
func sectionQuestion(sentence, heading string) port.Question {
	if heading == "" {
		heading = "(none)"
	}
	return port.Question{
		Type: port.Choice,
		Instructions: map[string]any{
			"sentence": sentence,
			"heading":  heading,
			"question": "Which part of the job description is `sentence`? " +
				"`heading` is the nearest heading above it in the posting. An item under a " +
				"heading such as \"Bonus points\" or \"Nice to have\" is preferred, and an " +
				"item under \"Benefits\" is benefits, even if the item itself does not say so.",
		},
		Criteria: sectionCriteria,
	}
}

// sectionQuestionID is the question ID for sentence i.
func sectionQuestionID(i int) string { return fmt.Sprintf("section_%d", i) }

// droppedSections are excluded from Candidate generation. `other` covers
// headings, legal/EEO text, location and application logistics.
var droppedSections = map[domain.Section]bool{
	domain.SectionCompany:  true,
	domain.SectionBenefits: true,
	domain.SectionOther:    true,
}

// lowSectionConfidence marks a Section label as uncertain in logs and metrics.
const lowSectionConfidence = 0.5
