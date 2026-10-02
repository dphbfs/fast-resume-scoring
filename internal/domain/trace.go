package domain

// Trace records the intermediate decisions of one extraction so a missing or
// wrong Requirement can be attributed to a pipeline stage. It is not part of
// the v1 Result contract.
type Trace struct {
	JobSummary TraceSummary       `json:"job_summary"`
	Sentences  []TraceSentence    `json:"sentences"`
	Chunks     []TraceChunk       `json:"chunks"`
	Refinement []TraceRequirement `json:"refinement"`
	// Merges are pairs of Requirement values merged as duplicates.
	Merges [][2]string `json:"merges"`
	// Groups are the Alternative Groups by value.
	Groups [][]string `json:"groups"`
}

// TraceSummary is the Job Summary used as Validation background.
type TraceSummary struct {
	Text     string `json:"text"`
	Fallback bool   `json:"fallback"`
	Reason   string `json:"reason,omitempty"`
}

// TraceSentence is one Context Sentence after Section labeling.
type TraceSentence struct {
	Ref        Ref     `json:"ref"`
	Text       string  `json:"text"`
	Heading    string  `json:"heading,omitempty"`
	Section    Section `json:"section"`
	Confidence float64 `json:"confidence"`
	// Dropped means the sentence produced no Chunks: its Section is dropped
	// or it is a heading.
	Dropped bool `json:"dropped"`
}

// TraceChunk is one Validation Round question and its answer.
type TraceChunk struct {
	Ref     Ref    `json:"ref"`
	Text    string `json:"text"`
	Options int    `json:"options"`
	// Selected is the chosen Candidate, empty when the Chunk was rejected.
	Selected        string  `json:"selected,omitempty"`
	SelectedP       float64 `json:"selected_p,omitempty"`
	RequirementMass float64 `json:"requirement_mass"`
	// RejectReason is the most probable rejection option when rejected.
	RejectReason string `json:"reject_reason,omitempty"`
	// Top holds the most probable options with their probabilities.
	Top []TraceOption `json:"top"`
	// Candidates lists every option offered for the Chunk.
	Candidates []string `json:"candidates"`
}

// TraceOption is one Choice option with its probability.
type TraceOption struct {
	Option string  `json:"option"`
	P      float64 `json:"p"`
}

// TraceRequirement is one validated Requirement in the Refinement Round.
type TraceRequirement struct {
	Value    string  `json:"value"`
	Mentions int     `json:"mentions"`
	Kept     bool    `json:"kept"`
	KeepP    float64 `json:"keep_p"`
	// FillerKind is the most probable Filler option when dropped.
	FillerKind string `json:"filler_kind,omitempty"`
	// DuplicateOf is the Requirement this one named as its duplicate, if any.
	DuplicateOf string `json:"duplicate_of,omitempty"`
	// MergedInto is the canonical value when a mutual duplicate merged it.
	MergedInto string `json:"merged_into,omitempty"`
	// AlternativeOf is the Requirement this one named as its alternative.
	AlternativeOf string `json:"alternative_of,omitempty"`
	// Score is the raw Importance Score; Importance is Score normalized.
	Score      float64 `json:"score"`
	Importance float64 `json:"importance"`
}

// CheckTrace records the Resume Checker's intermediate decisions, one entry
// per Evidence Unit. It is not part of the coverage contract.
type CheckTrace struct {
	Units []TraceUnit `json:"units"`
}

// TraceUnit is one Evidence Unit's Retrieval and Strength Round answers.
type TraceUnit struct {
	ID            string        `json:"id"`
	Text          string        `json:"text"`
	ResumeSection ResumeSection `json:"resume_section"`
	// Rounds are the Retrieval Round's Choice requests, in order.
	Rounds []TraceRound `json:"rounds"`
	// Retrieved are the Requirement values passed to the Strength Round.
	Retrieved []string    `json:"retrieved"`
	Pairs     []TracePair `json:"pairs"`
}

// TracePair is one Strength Round answer for a retrieved Requirement.
type TracePair struct {
	Requirement   string             `json:"requirement"`
	Probabilities map[string]float64 `json:"probabilities"`
	EvidenceMass  float64            `json:"evidence_mass"`
	// Gate is P(yes) of the gate Noul, when the gate is on.
	Gate     *float64         `json:"gate,omitempty"`
	Linked   bool             `json:"linked"`
	Strength EvidenceStrength `json:"strength,omitempty"`
	// RejectReason is the most probable non-evidence option (none or a
	// negative) when the pair was not linked.
	RejectReason string `json:"reject_reason,omitempty"`
	// Capped means the Resume Section cap lowered the strength to weak.
	Capped bool `json:"capped,omitempty"`
	// Vetoed means the gate passed but the grading Choice put at least
	// CHECKER_VETO_THRESHOLD on RejectReason, so the pair was not linked.
	Vetoed bool `json:"vetoed,omitempty"`
}

// TraceRound is one Retrieval Choice: how many options it offered, the most
// probable ones ("none" included), and the Requirements it kept.
type TraceRound struct {
	Options int           `json:"options"`
	Top     []TraceOption `json:"top"`
	Kept    []string      `json:"kept"`
}
