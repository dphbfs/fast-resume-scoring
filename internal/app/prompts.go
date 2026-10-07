package app

import (
	"github.com/dphbfs/fast-resume-scoring/internal/port"
	"github.com/dphbfs/fast-resume-scoring/tuning"
)

// prompts holds the tuning file's prompt text (tuning/tuning.yaml) in the
// shapes the questions send, built once per Extractor, Checker, or
// HolisticJudge. TestPromptSnapshot pins the resulting requests.
type prompts struct {
	sectionQuestionText string
	sectionCriteria     map[string]any
	summarySystem       string

	validationQuestionText string
	rejectOptions          map[string]string

	fillerQuestionText string
	fillerKeepText     string // description of the fillerKeep option
	fillerReasons      map[string]string

	duplicateQuestionText   string
	duplicateReasons        map[string]string
	alternativeQuestionText string
	alternativeReasons      map[string]string

	importanceQuestionText string
	importanceLevels       []any

	retrieval        tuning.Retrieval
	gate             tuning.Gate
	strengthTask     string
	strengthCriteria map[string]any

	holistic        map[string]port.Question
	roleMatchLevels int
}

func newPrompts(t *tuning.Tuning) *prompts {
	ex, ck := t.Extractor, t.Checker
	p := &prompts{
		sectionQuestionText: ex.Section.Question,
		sectionCriteria:     stringsToAny(ex.Section.Options),
		summarySystem:       ex.JobSummary.SystemPrompt,

		validationQuestionText: ex.Validation.Question,
		rejectOptions:          ex.Validation.RejectOptions,

		fillerQuestionText: ex.Refinement.Filler.Question,
		fillerKeepText:     ex.Refinement.Filler.Keep,
		fillerReasons:      ex.Refinement.Filler.Reasons,

		duplicateQuestionText:   ex.Refinement.Duplicate.Question,
		duplicateReasons:        ex.Refinement.Duplicate.Reasons,
		alternativeQuestionText: ex.Refinement.Alternative.Question,
		alternativeReasons:      ex.Refinement.Alternative.Reasons,

		importanceQuestionText: ex.Refinement.Importance.Question,
		importanceLevels:       levelsToAny(ex.Refinement.Importance.Levels),

		retrieval:        ck.Retrieval,
		gate:             ck.Gate,
		strengthTask:     ck.Strength.Task,
		strengthCriteria: map[string]any{},

		roleMatchLevels: len(t.Holistic.RoleMatch.Levels),
	}
	for name, o := range ck.Strength.Options {
		rubric := map[string]any{"what": o.What}
		if o.NotFor != "" {
			rubric["not_for"] = o.NotFor
		}
		if len(o.Examples) > 0 {
			rubric["examples"] = o.Examples
		}
		p.strengthCriteria[name] = rubric
	}
	h := t.Holistic
	p.holistic = map[string]port.Question{
		"role_match": {
			Type:         port.Score,
			Instructions: h.RoleMatch.Question,
			Criteria:     levelsToAny(h.RoleMatch.Levels),
		},
		"experience_short": noulQuestion(h.ExperienceShort),
		"blocker":          noulQuestion(h.Blocker),
	}
	return p
}

func noulQuestion(q tuning.NoulQuestion) port.Question {
	return port.Question{
		Type:         port.Noul,
		Instructions: q.Question,
		Criteria:     map[string]any{"true": q.IfTrue, "false": q.IfFalse},
	}
}

func stringsToAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func levelsToAny(levels []string) []any {
	out := make([]any, len(levels))
	for i, l := range levels {
		out[i] = l
	}
	return out
}
