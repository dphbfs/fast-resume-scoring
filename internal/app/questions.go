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

// --- Job Summary (stage 4) ---

// summarySystemPrompt instructs the generative model. The summary is
// background for judging Candidates, so it names the role, not requirements.
const summarySystemPrompt = "You summarize job postings. Reply with 2 or 3 plain sentences " +
	"describing the role: its title and seniority, the product or domain, and the core " +
	"technologies. No lists, no benefits, no company marketing, no requirements beyond the core stack."

// maxSummaryRunes caps the generated summary sent to Jev on every
// Validation request.
const maxSummaryRunes = 600

// maxFallbackRunes caps the fallback summary built from the title and the
// required/preferred sentences.
const maxFallbackRunes = 1500

// --- Validation Round (stage 5) ---

// validationQuestion asks what kind of phrase one Candidate is. A Choice
// over described situations separated answers better than a single Noul
// that bundled "complete", "specific", "asked of the applicant" and
// "single" into one judgment (live test: Nouls clustered near 0.5).
//
// The words around the Candidate are embedded too: judged alone, a word cut
// out of a longer name ("distributed" from "distributed systems") looks like
// a valid skill.
func validationQuestion(candidate, before, after string) port.Question {
	if before == "" {
		before = "(start of sentence)"
	}
	if after == "" {
		after = "(end of sentence)"
	}
	return port.Question{
		Type: port.Choice,
		Instructions: map[string]any{
			"candidate":    candidate,
			"words_before": before,
			"words_after":  after,
			"question": "In `sentence`, `candidate` appears between `words_before` and `words_after`. " +
				"What is `candidate`?",
		},
		Criteria: validationCriteria,
	}
}

// validationCriteria are the Candidate kinds. Keys in requirementKinds are
// accepted as Requirements.
var validationCriteria = map[string]any{
	"technology": "A specific tool, language, framework, platform, or standard, " +
		"e.g. \"Kafka\", \"Go\", \"PostgreSQL\", \"X.509\".",
	"skill_or_domain": "A specific skill, practice, or domain area, e.g. \"API design\", " +
		"\"distributed systems\", \"payments\", \"threat modeling\".",
	"experience_or_qualification": "A kind or amount of experience, a degree, or a " +
		"certification, e.g. \"5+ years of backend engineering\", \"Bachelor's degree in " +
		"Computer Science\", \"Security+\".",
	"responsibility": "A specific piece of work the role involves, e.g. \"SDK development\", " +
		"\"incident response\", \"mentoring engineers\".",
	"partial_or_padded": "Cut off: `words_before` or `words_after` continues the same name or " +
		"idea (\"distributed\" followed by \"systems\", \"development\" preceded by \"SDK\"). " +
		"Or padded: a requirement with extra words attached (\"use Vue\", \"Own SDK development\", " +
		"\"experience with Kafka\").",
	"several_items": "Two or more separate requirements joined together, e.g. \"Go and " +
		"Kubernetes\", \"Kafka Flink\".",
	"generic": "A generic word or phrase that is not a specific requirement, e.g. " +
		"\"strong\", \"experience\", \"team\", \"fast-paced environment\".",
}

// requirementKinds are the validationCriteria options that mean "this
// Candidate is a Requirement".
var requirementKinds = []string{"technology", "skill_or_domain", "experience_or_qualification", "responsibility"}

// acceptCandidate is the minimum probability mass on requirementKinds for a
// Candidate to become a validated Requirement. Tune with `make eval`.
const acceptCandidate = 0.5

// validationBatchSize caps questions per Validation request; a sentence with
// more Candidates is split into several requests with the same state.
const validationBatchSize = 150
