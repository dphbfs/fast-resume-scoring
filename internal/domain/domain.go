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

// Sections a ContextSentence can belong to. Sentences in company, benefits,
// and other produce no Requirements.
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
	// Ref is the key the sentence is stored under in Result.Context, so it
	// is not repeated in the JSON.
	Ref     Ref     `json:"-"`
	Text    string  `json:"text"`
	Section Section `json:"section"`
}

// Candidate is a sliding-window phrase from a ContextSentence that has not
// yet been judged.
type Candidate struct {
	// Text is the phrase as it appears in the sentence.
	Text string
	// Ref is the ContextSentence the phrase came from.
	Ref Ref
}

// Tier is how firmly the employer asks for a Requirement. It is independent
// of Importance.
type Tier string

// Tiers, strongest first.
const (
	TierRequired  Tier = "required"
	TierPreferred Tier = "preferred"
	// TierMentioned covers Requirements named only in responsibilities (or
	// any other kept Section).
	TierMentioned Tier = "mentioned"
)

// TierFor returns the Tier of a Requirement mentioned in the given Sections:
// the strongest one wins (required > preferred > anything else).
func TierFor(sections []Section) Tier {
	tier := TierMentioned
	for _, s := range sections {
		switch s {
		case SectionRequired:
			return TierRequired
		case SectionPreferred:
			tier = TierPreferred
		default: // responsibilities and the rest keep the current tier
		}
	}
	return tier
}

// Requirement is one atomic thing the employer asks for.
type Requirement struct {
	// ID is unique within the Result (e.g. "req_1").
	ID string `json:"id"`
	// Value is the phrase as written in the posting, e.g. "Go in production".
	Value string `json:"value"`
	// Refs are the ContextSentences that mention the Requirement, each a key
	// of Result.Context.
	Refs []Ref `json:"refs"`
	// Tier is derived from the Sections of Refs (see TierFor).
	Tier Tier `json:"tier"`
	// Importance is Jev's Score normalized to 0..1; 0 when the Result's
	// ImportanceSkipped is set.
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

// Result is the Requirement Extractor's output (schema v1; sample:
// examples/requirements.json).
type Result struct {
	// SchemaVersion holds the SchemaVersion constant.
	SchemaVersion string `json:"schema_version"`
	// Model is the Jev model that produced the Result.
	Model string `json:"model"`
	// Requirements are ordered by Importance, highest first, unless
	// ImportanceSkipped is set.
	Requirements      []Requirement      `json:"requirements"`
	AlternativeGroups []AlternativeGroup `json:"alternative_groups"`
	// Context holds only the sentences that Requirements refer to, by Ref.
	Context map[Ref]ContextSentence `json:"context"`
	// ImportanceSkipped means Importance was not asked (scoring mode, which
	// does not use it): every Requirement's Importance is 0, and
	// Requirements keep their order of first mention.
	ImportanceSkipped bool `json:"importance_skipped,omitempty"`
}
