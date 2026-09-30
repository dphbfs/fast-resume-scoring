// Package domain holds the Requirement Extractor's core types. Names follow
// the glossary in CONTEXT.md.
package domain

// JobDescription is the plain-text posting a run analyzes.
type JobDescription struct {
	// Title is the job title, taken from the first line of the input file.
	Title string
	// Text is the full posting, including the title line.
	Text string
}

// Ref identifies a ContextSentence within one extraction Result (e.g. "s3").
type Ref string

// Section is the role a ContextSentence plays in the JobDescription.
type Section string

const (
	SectionRequired         Section = "required"
	SectionPreferred        Section = "preferred"
	SectionResponsibilities Section = "responsibilities"
	SectionCompany          Section = "company"
	SectionBenefits         Section = "benefits"
	SectionOther            Section = "other"
)

// Sections lists every Section in a stable order, for building Jev Choice
// criteria.
var Sections = []Section{
	SectionRequired,
	SectionPreferred,
	SectionResponsibilities,
	SectionCompany,
	SectionBenefits,
	SectionOther,
}

// ContextSentence is one sentence of the JobDescription, stored once under
// its Ref.
type ContextSentence struct {
	Ref     Ref     `json:"-"`
	Text    string  `json:"text"`
	Section Section `json:"section"`
}

// Candidate is a sliding-window phrase from a ContextSentence that has not
// yet been judged.
type Candidate struct {
	Text string
	Ref  Ref
	// Start and End are word offsets within the sentence, End exclusive.
	Start, End int
}

// Requirement is one atomic thing the employer asks for.
type Requirement struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Refs  []Ref  `json:"refs"`
	// Importance is Jev's Score normalized to 0..1.
	Importance float64 `json:"importance"`
}

// AlternativeGroup is a set of Requirements where the employer accepts any
// one of them. Members are Requirement IDs.
type AlternativeGroup struct {
	ID      string   `json:"id"`
	Members []string `json:"members"`
}

// SchemaVersion is the version of the Result JSON contract.
const SchemaVersion = "1"

// Result is the Requirement Extractor's output (schema v1, see CLAUDE.md).
type Result struct {
	SchemaVersion     string                  `json:"schema_version"`
	Model             string                  `json:"model"`
	Requirements      []Requirement           `json:"requirements"`
	AlternativeGroups []AlternativeGroup      `json:"alternative_groups"`
	Context           map[Ref]ContextSentence `json:"context"`
}
