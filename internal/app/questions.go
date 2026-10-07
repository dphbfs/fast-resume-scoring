package app

import (
	"fmt"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
)

// This file builds every Requirement Extractor question and holds the
// policy constants applied to the answers. The prompt text comes from the
// tuning file (tuning/tuning.yaml) through prompts.

// --- Section labeling (stage 2) ---

// sectionQuestion asks which Section one sentence belongs to. The sentence
// and its nearest heading are embedded in the question itself: Jev cannot
// reliably find a sentence by its position in a list (live test, jev-1.13
// labeled neighbouring sentences).
func (p *prompts) sectionQuestion(sentence, heading string) port.Question {
	if heading == "" {
		heading = "(none)"
	}
	return port.Question{
		Type: port.Choice,
		Instructions: map[string]any{
			"sentence": sentence,
			"heading":  heading,
			"question": p.sectionQuestionText,
		},
		Criteria: p.sectionCriteria,
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

// maxSummaryRunes caps the generated summary sent to Jev on every
// Validation request.
const maxSummaryRunes = 600

// maxFallbackRunes caps the fallback summary built from the title and the
// required/preferred sentences.
const maxFallbackRunes = 1500

// --- Validation Round (stage 5) ---

// validationQuestion asks Jev to select the option that names the
// Requirement in one chunk, or to say which kind of non-requirement it is
// (the reject options: several give "no" as many ways to be described as
// "yes" has; with a single option, a padded phrase like "Work closely"
// often won). Selecting among a chunk's overlapping spans is a relative
// judgment; judging each span alone accepted cut-off words ("financial"
// from "financial systems") in live tests.
func (p *prompts) validationQuestion(c chunk) port.Question {
	criteria := make(map[string]any, len(c.Options)+len(p.rejectOptions))
	for _, o := range c.Options {
		criteria[o] = nil
	}
	for k, v := range p.rejectOptions {
		criteria[k] = v
	}
	return port.Question{
		Type: port.Choice,
		Instructions: map[string]any{
			"chunk":    c.Text,
			"question": p.validationQuestionText,
		},
		Criteria: criteria,
	}
}

// validationBatchSize caps questions per Validation request; a sentence with
// more chunks is split into several requests with the same state.
const validationBatchSize = 150

// --- Refinement Round (stage 5) ---
//
// Every Refinement question embeds its Requirement and all its mentions
// (sentence + Section), so recurrence and Section are explicit data and Jev
// never has to look anything up by position.

// mention is one sentence where a Requirement appears.
type mention struct {
	Section  domain.Section `json:"section"`
	Sentence string         `json:"sentence"`
}

func refinementInstructions(requirement string, mentions []mention, question string) map[string]any {
	return map[string]any{"requirement": requirement, "mentions": mentions, "question": question}
}

// fillerKeep is the option meaning the Requirement is kept.
const fillerKeep = "specific_requirement"

// fillerQuestion asks whether a Requirement is specific enough to check
// against a resume, or which kind of Filler it is.
func (p *prompts) fillerQuestion(requirement string, mentions []mention) port.Question {
	criteria := map[string]any{fillerKeep: p.fillerKeepText}
	for k, v := range p.fillerReasons {
		criteria[k] = v
	}
	return port.Question{
		Type:         port.Choice,
		Instructions: refinementInstructions(requirement, mentions, p.fillerQuestionText),
		Criteria:     criteria,
	}
}

// minKeepMass is the minimum probability on fillerKeep to keep a Requirement.
const minKeepMass = 0.5

// duplicateQuestion asks which other Requirement names the same thing. The
// reason options each name a failure mode seen in eval, where Jev merged
// related Requirements that are not synonyms.
func (p *prompts) duplicateQuestion(requirement string, mentions []mention, options []string) port.Question {
	criteria := make(map[string]any, len(options)+len(p.duplicateReasons))
	for _, o := range options {
		criteria[o] = nil
	}
	for k, v := range p.duplicateReasons {
		criteria[k] = v
	}
	return port.Question{
		Type:         port.Choice,
		Instructions: refinementInstructions(requirement, mentions, p.duplicateQuestionText),
		Criteria:     criteria,
	}
}

// minMergeMass is the minimum probability on the Requirement options to
// link a duplicate. Two Requirements merge only when each links the other.
const minMergeMass = 0.7

// maxAllDuplicateOptions is the list size up to which every other
// Requirement is offered as a duplicate; beyond it only similar ones are.
const maxAllDuplicateOptions = 40

// alternativeQuestion asks which Requirement from the same sentence is
// offered as an interchangeable alternative.
func (p *prompts) alternativeQuestion(requirement string, mentions []mention, options []string) port.Question {
	criteria := make(map[string]any, len(options)+len(p.alternativeReasons))
	for _, o := range options {
		criteria[o] = nil
	}
	for k, v := range p.alternativeReasons {
		criteria[k] = v
	}
	return port.Question{
		Type:         port.Choice,
		Instructions: refinementInstructions(requirement, mentions, p.alternativeQuestionText),
		Criteria:     criteria,
	}
}

// minAlternativeMass is the minimum probability on the Requirement options
// to link an alternative.
const minAlternativeMass = 0.7

// importanceQuestion asks how much the employer cares about a Requirement;
// Importance is the Score divided by the top level index.
func (p *prompts) importanceQuestion(requirement string, mentions []mention) port.Question {
	return port.Question{
		Type:         port.Score,
		Instructions: refinementInstructions(requirement, mentions, p.importanceQuestionText),
		Criteria:     p.importanceLevels,
	}
}

// refinementBatchChars caps the JSON size of the questions in one Refinement
// request (~4 chars per token), keeping requests under OpenRouter's 32k-token
// limit with room for the state.
const refinementBatchChars = 80_000
