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
  - `AIClassifierClient`: Jev via the System One API (`POST /v1/systemone`).
    Does all judging. Served through **OpenRouter**:
    `TYPESAFE_BASE_URL=https://openrouter.ai/api`,
    `TYPESAFE_API_KEY=<OpenRouter key>`, `JEV_MODEL=jev-1.13` (or
    `jev-latest`). Same request/response shapes as api.typesafe.ai; limit is
    32k tokens per request on OpenRouter.
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
   `company` / `benefits` / `other` sentences are dropped before windowing
3. Chunking and Candidate generation (code only): each kept sentence is cut
   into Chunks at clause breaks (commas, brackets, dashes) and list words
   (`and`, `or`, `with`, `such as`, `including`, `e.g.`, `like`). A Chunk's
   Candidates are its pruned windows (1..`PIPELINE_MAX_WINDOW_WORDS` words,
   no leading/trailing stopwords; slash-joined names both split and whole)
   plus the whole Chunk when it has <= 8 words.
4. Validation Round: one Jev request per Context Sentence (state: Section +
   sentence + Job Summary from `AIGenerativeClient`, generated once per run),
   one Choice per Chunk: "which option names the requirement in `chunk`
   completely and without extra words?", options = its Candidates +
   `no_requirement`. The selected Candidate becomes a validated Requirement.
   Requests run through the bounded concurrency layer.
   - Why select, not judge: judging each window alone (Noul, or a Choice of
     kinds) accepted cut-off words like "financial" from "financial
     systems" in live tests.
   - The compound check was removed: Chunks never overlap, so a selection
     cannot contain other selections.
   - Options also include four rejection kinds (generic_trait,
     people_or_context, action_only, condition); a Chunk is accepted when its
     Candidates hold >= `PIPELINE_MIN_REQUIREMENT_MASS` (default 0.7) of the
     probability. Heading lines and generic-only Candidates ("Hands-on
     experience") are never offered.
   - Open tuning: long whole-Chunk picks; splitting at "and" cuts some years
     qualifiers.
5. Refinement Round: state is only the job title + Job Summary. Every
   question embeds its Requirement and all its mentions (sentence + Section),
   so recurrence and Section are explicit data. Questions are batched by
   size (< 32k tokens per request for OpenRouter). Per Requirement:
   - Filler (Choice): `specific_requirement` vs vague_term / generic_trait /
     company_context / condition; kept when `specific_requirement` >= 0.5.
   - Duplicate (Choice): other Requirements (all when <= 40, else similar
     ones) vs different_thing / broader_or_narrower / part_of_or_contains /
     related_not_same. Merged only when **both** name each other (one-way
     links were mostly related-but-different pairs); canonical value = most
     mentions, then first seen.
   - Alternative (Choice): Requirements sharing a sentence vs
     required_together / unrelated; links become Alternative Groups by
     connected components.
   - Importance (Score, 5 situation levels from "mentioned in passing" to
     "hard requirement, emphasized"); Importance = score / 4, merged
     Requirements keep the max.
6. Output: JSON contract below. `extract -debug trace.json` writes the
   extraction trace (`domain.Trace`): Job Summary, every sentence with
   heading/Section/confidence/dropped, every Chunk with its Candidates, top
   probabilities, selection or reject reason, every Refinement answer
   (Filler kind, duplicate/alternative links, Importance), merges and groups.
   `Extract` always returns the trace; callers ignore it when not needed.

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
  (needs `TYPESAFE_API_KEY`) and writes a dated report. Keys live in a local,
  gitignored `.env`; never commit them.

## Eval

- `make eval` builds `cmd/eval`, loads `.env`, runs every golden fixture live
  (4 in parallel), and writes `eval/reports/<UTC timestamp>.{json,md}`
  (committed, so changes can be compared). `EVAL_ARGS="-only <id-prefix>"`
  runs a subset.
- Labels follow the rules in `testdata/golden/README.md` (only employer
  text; every named specific item is a Requirement; soft skills and long
  duty clauses are `acceptable`; conditions, traits, headings, job titles
  are `filler`; synonyms are one entry with aliases). `go test` lints the
  labels (`TestGoldenLabelsAreConsistent`).
- Scoring (`internal/adapter/eval`): one-to-one matching; strict =
  value/alias equality after normalization; loose also accepts padding
  (<= 3 extra words), a multi-word label shortened by one word, or token
  Jaccard >= 0.6; words are split at `/` and `-` and compared by stem.
  Buckets: match, miss, extra (counts against precision), filler (reported,
  counts against precision), acceptable and duplicate (reported, excluded
  from precision). Reports carry a labels fingerprint; compare runs only
  when it matches.
- `eval -rescore <report.json>` re-scores stored results against the current
  labels with no API calls; use it after any label edit.
- Eval writes each fixture's trace to `eval/reports/<run>-traces/`
  (gitignored) and attributes every miss to a stage (section_dropped,
  no_candidate, validation_rejected, validation_not_selected,
  refinement_filler, refinement_merged, scoring). Rescore reloads the traces.
- History (no Refinement Round yet), 2026-09-30:
  - baseline, one `no_requirement` option: recall 90.6% / precision 27.4%
  - four rejection options, decide on summed mass: 91.4% / 30.8%
  - skip headings, drop generic-only Candidates: 91.2% / 32.8%
  - `PIPELINE_MIN_REQUIREMENT_MASS` sweep 0.5-0.9: recall flat to 0.7, then
    falls (88.8% at 0.8, 86.7% at 0.9); default 0.7 -> 91.2% / 35.2%
  - Refinement Round v1 (one-way duplicate merges): 81.0% / 42.5%, Filler
    extracted 1, tier order 85.2%, group F1 55.0%; 274 merges, mostly
    related-but-different pairs
  - v2, mutual duplicates only: 85.9% / 40.3%, tier order 85.7%, group F1
    66.9%, 58 merges. Misses: ~14 from Filler drops ("Infrastructure as
    Code", "SIEM"), ~17 where labels keep related items separate. These
    Refinement runs used the fallback Job Summary (generative endpoint
    unreachable).
  - v2 rerun with real Job Summaries: 86.5% (strict 71.4%) / 40.8%, tier
    order 83.4%, group F1 63.0%, ~$0.0056 Jev per posting. The fallback
    confound was small (~0.6 recall); Refinement itself trades ~5 recall
    points for ~6 precision points and near-zero Filler.
- Runs vary by ~1 point because the Job Summary is regenerated each run.
- Label review 2026-09-30 (labels `63732feae2fb`, 668 Requirements): the
  numbers above used the old labels and are not comparable. The last clean
  run rescored: recall 93.6% (strict 79.6%), precision 87.5%, F1 90.4%;
  276 acceptable, 92 duplicates, 78 extras. Caveat: missing labels were
  found by pooling the extractor's own extras, so Requirements that neither
  the labels nor the extractor found stay invisible.

- Next tuning round: `docs/tuning.md` (prioritized backlog and experiment
  log; record every experiment there).

## Test data

- Job Descriptions come from the Reactive Resume MCP server (`reactive-resume`,
  local-scope config). Pull Job Descriptions only, never resumes; strip
  recruiter names and emails.
- `testdata/jd/<id>.txt` plus a metadata sidecar (source, date, title).
- Golden set: ~20 hand-reviewed fixtures with expected Requirements, committed.
  Claude drafts the labels, the user corrects them.
- Bulk set: gitignored, fetched by a script, used for smoke/regression runs
  (no crash, valid schema, stats), not accuracy.

## Code layout (hexagonal, wired with google/wire)

- `cmd/extract/`: entry point. `wire.go` is the injector (`//go:build
  wireinject`); `wire_gen.go` is generated and committed. Run `make wire`
  after changing any constructor signature; `make wire-check` validates.
- `internal/domain/`: core types named after `CONTEXT.md` terms, plus the
  schema v1 `Result`.
- `internal/port/`: interfaces the core depends on
  (`AIClassifierClient`, `AIGenerativeClient`, `Metrics`) and the driving port
  `RequirementExtractor`.
- `internal/app/`: the pipeline (`Extractor`); one method per stage.
- `internal/adapter/`: `jev` (TypeSafe HTTP client, retries, own limiter),
  `jev/jevtest` (fake Jev server for tests), `openai` (OpenAI-compatible chat),
  `cli` (driving adapter).
- `internal/platform/`: `config` (env vars), `limiter`, `metrics` (CLI run
  summary), `logging` (slog JSON, `LOG_LEVEL`).
- Commands: `make test`, `make race`, `make vet`, `make build`, `make wire`.

## Go skills

Go skills from `samber/cc-skills-golang` are symlinked into `.claude/skills/`.
Use the relevant `golang-*` skills when writing, reviewing, or testing Go code.
Always load the `golang-how-to` skill for any Go coding, review, debug, or
setup task; it selects the other `golang-*` skills to load.
