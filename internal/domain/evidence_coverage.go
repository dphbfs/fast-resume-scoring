package domain

// EvidenceStrength is how well an Evidence Unit demonstrates a Requirement.
// StrengthNone is used only as a Coverage value: a pair with no evidence
// gets no EvidenceLink.
type EvidenceStrength string

const (
	StrengthStrong  EvidenceStrength = "strong"
	StrengthPartial EvidenceStrength = "partial"
	StrengthWeak    EvidenceStrength = "weak"
	StrengthNone    EvidenceStrength = "none"
)

// Rank orders strengths: none 0, weak 1, partial 2, strong 3.
func (s EvidenceStrength) Rank() int {
	switch s {
	case StrengthStrong:
		return 3
	case StrengthPartial:
		return 2
	case StrengthWeak:
		return 1
	}
	return 0
}

// EvidenceLink pairs a Requirement with an Evidence Unit that demonstrates
// it. P is Jev's probability that the unit is evidence at all
// (strong + partial + weak).
type EvidenceLink struct {
	Unit     string           `json:"unit"`
	Strength EvidenceStrength `json:"strength"`
	P        float64          `json:"p"`
}

// RequirementCoverage is one Requirement with its Evidence Links and
// Coverage (the best link's strength, or none).
type RequirementCoverage struct {
	ID       string           `json:"id"`
	Value    string           `json:"value"`
	Tier     Tier             `json:"tier,omitempty"`
	Coverage EvidenceStrength `json:"coverage"`
	Evidence []EvidenceLink   `json:"evidence"`
}

// GroupCoverage is an Alternative Group with its best member's Coverage.
type GroupCoverage struct {
	ID       string           `json:"id"`
	Members  []string         `json:"members"`
	Coverage EvidenceStrength `json:"coverage"`
}

// CoverageSchemaVersion is the version of the CoverageResult JSON contract.
const CoverageSchemaVersion = "1"

// CoverageResult is the Resume Checker's output (coverage schema v1, see
// CLAUDE.md).
type CoverageResult struct {
	SchemaVersion     string                  `json:"schema_version"`
	Model             string                  `json:"model"`
	Requirements      []RequirementCoverage   `json:"requirements"`
	AlternativeGroups []GroupCoverage         `json:"alternative_groups"`
	Fit               Fit                     `json:"fit"`
	EvidenceUnits     map[string]EvidenceUnit `json:"evidence_units"`
}
