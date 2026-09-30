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

// rejectOptions are the Choice options meaning the chunk names no
// Requirement, each describing one kind of non-requirement. Several options
// give "no" as many ways to be described as "yes" has (one per Candidate);
// with a single option, a padded phrase like "Work closely" often won.
var rejectOptions = map[string]string{
	"generic_trait": "`chunk` is a generic trait, attitude, or work style rather than a specific " +
		"skill: \"strong judgment\", \"self-starter\", \"fast-moving environment\", \"team player\", " +
		"\"attention to detail\".",
	"people_or_context": "`chunk` names people, teams, the company, its product, or its customers " +
		"rather than something the applicant must know or do: \"product managers\", \"partner " +
		"teams\", \"our customers\", \"the platform\".",
	"action_only": "`chunk` is only a verb or a vague activity with no specific skill or " +
		"technology: \"Work closely\", \"Collaborate\", \"iterate rapidly\", \"ship features\", " +
		"\"take ownership\".",
	"condition": "`chunk` is a condition of the job rather than a skill: location, time zone, " +
		"work authorization, citizenship, clearance, travel, schedule, or on-call.",
}

// validationQuestion asks Jev to select the option that names the
// Requirement in one chunk, or to say which kind of non-requirement it is.
// Selecting among a chunk's overlapping spans is a relative judgment; judging
// each span alone accepted cut-off words ("financial" from "financial
// systems") in live tests.
func validationQuestion(c chunk) port.Question {
	criteria := make(map[string]any, len(c.Options)+len(rejectOptions))
	for _, o := range c.Options {
		criteria[o] = nil
	}
	for k, v := range rejectOptions {
		criteria[k] = v
	}
	return port.Question{
		Type: port.Choice,
		Instructions: map[string]any{
			"chunk": c.Text,
			"question": "`chunk` is part of `sentence` in a job posting. If `chunk` states a specific " +
				"skill, technology, qualification, kind of experience, or responsibility that a resume " +
				"could show, which option names it completely and without extra words? Prefer the full " +
				"name (\"distributed systems\", not \"distributed\"), and leave out words like " +
				"\"experience\", \"own\", \"use\" or \"strong\" around it. Otherwise, which kind " +
				"of non-requirement is `chunk`?",
		},
		Criteria: criteria,
	}
}

// validationBatchSize caps questions per Validation request; a sentence with
// more chunks is split into several requests with the same state.
const validationBatchSize = 150
