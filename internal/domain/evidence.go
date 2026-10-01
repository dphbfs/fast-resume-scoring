package domain

// Resume is the plain-text resume a Resume Checker run reads.
type Resume struct {
	Text string
}

// ResumeSection is a headed part of the Resume. Headings are mapped onto
// these kinds; anything unrecognized is ResumeOther.
type ResumeSection string

const (
	ResumeExperience     ResumeSection = "experience"
	ResumeProjects       ResumeSection = "projects"
	ResumeSkills         ResumeSection = "skills"
	ResumeSummary        ResumeSection = "summary"
	ResumeEducation      ResumeSection = "education"
	ResumeCertifications ResumeSection = "certifications"
	ResumeOther          ResumeSection = "other"
)

// EvidenceUnit is one whole piece of the Resume, kept verbatim: a bullet, a
// prose sentence, a skills line, or an education or certification entry.
// Role, Company, and Dates come from the entry header above it, when any.
type EvidenceUnit struct {
	ID            string        `json:"-"`
	Text          string        `json:"text"`
	ResumeSection ResumeSection `json:"resume_section"`
	Role          string        `json:"role,omitempty"`
	Company       string        `json:"company,omitempty"`
	Dates         string        `json:"dates,omitempty"`
}
