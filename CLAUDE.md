# fast-resume-tailoring

## Goal

A Go-based tool that uses Jev to extract and prioritize the Requirements in a
Job Description. Later, a Resume Checker will mark each Requirement present or
missing in a resume.

Jev is TypeSafe AI's System One model (https://docs.typesafe.ai/introduction):
a fast, typed classifier, not a text generator. Use the official TypeSafe
skill (`/typesafe:typesafe-ai`, plugin `typesafe@typesafe-ai`) for anything
that calls Jev or designs its questions.

## Architecture

- A concurrent Go harness orchestrates the pipeline.
- Two AI client interfaces, so providers stay swappable and tests use fakes:
  - `AIClassifierClient`: Jev (TypeSafe `/v1/systemone`). Does all judging.
  - `AIGenerativeClient`: any OpenAI-compatible chat API. In V1 it only writes
    the Job Summary; it never judges Candidates or Requirements. Configured
    via `OPENAI_BASE_URL`, `OPENAI_API_KEY`, `OPENAI_MODEL` (no default model).
    Optional: if unconfigured or the call fails, fall back to the job title +
    `required`/`preferred` sentences as background and log a warning.
- Each client has its own bounded concurrency limit (same limiter
  abstraction), configured separately.
- The Job Summary is used only in the Validation Round, not in the
  Refinement Round (revisit after eval).

## Pipeline

Domain terms are defined in `CONTEXT.md`; use them exactly.

V1 scope is the **Requirement Extractor** only, run as a CLI on a plain-text
Job Description (`.txt` / `.md` file):

1. Job Description → sentence split (Context Sentences with Refs)
2. Section labeling: one Jev Choice per sentence (`required | preferred |
   responsibilities | company | benefits | other`) in one batched request;
   `company` / `benefits` sentences are dropped before windowing
3. Sliding-window Candidate generation, pruned in code (no leading/trailing
   stopwords, max window length from config)
4. Validation Round: one Jev request per Context Sentence (state: Section +
   sentence + Job Summary from `AIGenerativeClient`, generated once per run),
   one Noul per Candidate asking whether it is a single atomic Requirement. Requests run through the bounded concurrency layer.
   - Compound check (early versions only): if an accepted Candidate contains
     two or more other accepted Candidates, drop it and **log a warning**. Any
     hit means Jev failed the task or its answer was misread, so treat it as a
     bug signal, not normal flow.
5. Refinement Round: one batched request. State is the validated Requirements,
   each with all its Context Sentences and their Sections, so recurrence and
   Section are explicit data (Jev can't count). Per Requirement:
   - Choice: which other Requirement it duplicates, or `none` (synonym merge)
   - Choice: which other Requirement it is offered as an alternative to
     ("Go, Ruby, or Python"), or `none`; code builds Alternative Groups from
     these links (connected components)
   - Noul: specific, checkable qualification vs. Filler (Filler is dropped;
     this includes non-skill conditions like work eligibility or on-call)
   - Score: Importance, from "mentioned in passing" to "stated as mandatory"
6. Output: JSON contract below; `--debug` writes dropped Candidates, Filler,
   merges and raw probabilities to a separate file.

Importance is Jev's Score only (normalized to 0–1), with no code-side formula.
**Verify in a real-world test** that Jev reflects recurrence and Section this
way; add a code-side formula only if it doesn't.

### Output contract (schema v1)

```json
{
  "schema_version": "1",
  "model": "jev-1.13.0",
  "requirements": [
    { "id": "req_1", "value": "Kubernetes", "refs": ["s3", "s9"], "importance": 0.82 },
    { "id": "req_2", "value": "Go", "refs": ["s5"], "importance": 0.74 },
    { "id": "req_3", "value": "Ruby", "refs": ["s5"], "importance": 0.74 }
  ],
  "alternative_groups": [
    { "id": "alt_1", "members": ["req_2", "req_3"] }
  ],
  "context": {
    "s3": { "text": "5+ years of backend experience with Go and Kubernetes in production.", "section": "required" }
  }
}
```

Requirements are sorted by `importance`, descending. A Requirement belongs to
at most one Alternative Group; the Resume Checker will treat a group as
satisfied when any member is present.

Later (not V1): the Resume Checker (resume parsing, cheap local matching,
Jev semantic matching, strict present/missing per Requirement), running
inside a larger resume-tailoring backend.

## Deferred ideas (keep for the Resume Checker)

- Requirement category (technology / experience / education / certification /
  soft skill / domain), assigned by a Jev Choice, so each type can be checked
  differently.
- Record Jev's raw probability and the resume evidence line with each
  present/missing outcome.

## V1 non-negotiables

- Structured logs and metrics throughout every pipeline stage: `slog` JSON to
  stderr per stage; metrics behind a small interface whose CLI implementation
  prints a run summary (counts per stage, Jev calls, tokens, latency p50/p95,
  retries), so an OTel/Prometheus exporter can plug in later.
- A bounded concurrency layer in front of Jev.
- Automated tests. `go test` uses a fake Jev server with recorded responses and
  never calls the live API. `make eval` runs the golden set against live Jev
  (needs `TYPESAFE_API_KEY`) and writes a dated report.

## Test data

- Job Descriptions come from the the generative scorer MCP server (`job-tracker`,
  local-scope config). Pull Job Descriptions only, never resumes; strip
  recruiter names and emails.
- `testdata/jd/<id>.txt` plus a metadata sidecar (source, date, title).
- Golden set: ~20 hand-reviewed fixtures with expected Requirements, committed.
  Claude drafts the labels, the user corrects them.
- Bulk set: gitignored, fetched by a script, used for smoke/regression runs
  (no crash, valid schema, stats), not accuracy.

## Go skills

Go skills from `samber/cc-skills-golang` are symlinked into `.claude/skills/`.
Use the relevant `golang-*` skills when writing, reviewing, or testing Go code.
