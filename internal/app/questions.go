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

// fillerReasons are the Filler kinds a Requirement can be dropped as.
var fillerReasons = map[string]string{
	"vague_term": "Too broad to check on a resume by itself: \"backend\", \"scalable\", " +
		"\"resilience\", \"operations\", \"quality\", \"integration\".",
	"generic_trait": "A personal trait, attitude, or work style: \"team player\", \"self-starter\", " +
		"\"strong judgment\", \"fast-paced environment\", \"ownership\".",
	"company_context": "A name or idea specific to this company, its internal systems, teams, or " +
		"customers rather than a transferable skill: \"FinHub\", \"Overseer's integrity " +
		"guarantees\", \"our merchants\".",
	"condition": "A condition of the job rather than a skill: work authorization, citizenship, " +
		"clearance, location, time zone, travel, schedule, or on-call rotation.",
}

// fillerQuestion asks whether a Requirement is specific enough to check
// against a resume, or which kind of Filler it is.
func fillerQuestion(requirement string, mentions []mention) port.Question {
	criteria := map[string]any{
		fillerKeep: "A specific skill, technology, domain, qualification, kind of experience, or " +
			"responsibility that a resume could show: \"Kubernetes\", \"payments\", \"5+ years of Go\", " +
			"\"API design\", \"mentoring engineers\".",
	}
	for k, v := range fillerReasons {
		criteria[k] = v
	}
	return port.Question{
		Type: port.Choice,
		Instructions: refinementInstructions(requirement, mentions,
			"`requirement` was extracted from the job posting sentences in `mentions`. What is it?"),
		Criteria: criteria,
	}
}

// minKeepMass is the minimum probability on fillerKeep to keep a Requirement.
const minKeepMass = 0.5

// duplicateReasons are the options meaning "no other option is the same".
// Each names a failure mode seen in eval, where Jev merged related
// Requirements that are not synonyms.
var duplicateReasons = map[string]string{
	"different_thing": "Every option names something different from `requirement` " +
		"(\"Kafka\" and \"Kafka Streams\", \"Go\" and \"Rust\").",
	"broader_or_narrower": "The closest option is a broader or narrower concept, not the same " +
		"thing (\"cloud\" and \"AWS\", \"Master's degree\" and \"Master's degree in " +
		"Computer Science\").",
	"part_of_or_contains": "The closest option contains `requirement` or is contained in it, " +
		"so one is more specific (\"dashboards\" and \"Grafana dashboards\", \"tools\" and " +
		"\"agentic engineering tools\").",
	"related_not_same": "The closest option is related or used together with `requirement` but " +
		"is a different skill (\"Terraform\" and \"Infrastructure as Code\", \"API " +
		"boundaries\" and \"API contracts\", \"security\" and \"secure development\").",
}

// duplicateQuestion asks which other Requirement names the same thing.
func duplicateQuestion(requirement string, mentions []mention, options []string) port.Question {
	criteria := make(map[string]any, len(options)+len(duplicateReasons))
	for _, o := range options {
		criteria[o] = nil
	}
	for k, v := range duplicateReasons {
		criteria[k] = v
	}
	return port.Question{
		Type: port.Choice,
		Instructions: refinementInstructions(requirement, mentions,
			"Which option names the same thing as `requirement`: a synonym, abbreviation, or "+
				"different spelling (\"K8s\" and \"Kubernetes\", \"Postgres\" and \"PostgreSQL\")?"),
		Criteria: criteria,
	}
}

// minMergeMass is the minimum probability on the Requirement options to
// link a duplicate. Two Requirements merge only when each links the other.
const minMergeMass = 0.7

// maxAllDuplicateOptions is the list size up to which every other
// Requirement is offered as a duplicate; beyond it only similar ones are.
const maxAllDuplicateOptions = 40

// alternativeReasons are the options meaning "not an alternative".
var alternativeReasons = map[string]string{
	"required_together": "The sentence asks for `requirement` together with the others, not " +
		"either one (\"Go and Kubernetes\").",
	"unrelated": "No option is offered as an alternative to `requirement`; they are listed for " +
		"different purposes.",
}

// alternativeQuestion asks which Requirement from the same sentence is
// offered as an interchangeable alternative.
func alternativeQuestion(requirement string, mentions []mention, options []string) port.Question {
	criteria := make(map[string]any, len(options)+len(alternativeReasons))
	for _, o := range options {
		criteria[o] = nil
	}
	for k, v := range alternativeReasons {
		criteria[k] = v
	}
	return port.Question{
		Type: port.Choice,
		Instructions: refinementInstructions(requirement, mentions,
			"In `mentions`, which option does the employer accept instead of `requirement`, so "+
				"that having either one is enough (\"Go, Ruby, or Python\", \"AWS or GCP\")?"),
		Criteria: criteria,
	}
}

// minAlternativeMass is the minimum probability on the Requirement options
// to link an alternative.
const minAlternativeMass = 0.7

// importanceLevels describe situations, lowest first; Importance is the
// Score divided by the top level index.
var importanceLevels = []any{
	"Mentioned only in passing: an example in a list, a tech-stack entry, or context.",
	"Part of the day-to-day work the posting describes, but not stated as a requirement.",
	"Listed as preferred, a bonus, a plus, or nice to have.",
	"Stated as a requirement for the role.",
	"Stated as a hard requirement and emphasized or repeated in several places.",
}

// importanceQuestion asks how much the employer cares about a Requirement.
func importanceQuestion(requirement string, mentions []mention) port.Question {
	return port.Question{
		Type: port.Score,
		Instructions: refinementInstructions(requirement, mentions,
			"How important is `requirement` to the employer, judging by every sentence in "+
				"`mentions` and its section?"),
		Criteria: importanceLevels,
	}
}

// refinementBatchChars caps the JSON size of the questions in one Refinement
// request (~4 chars per token), keeping requests under OpenRouter's 32k-token
// limit with room for the state.
const refinementBatchChars = 80_000
