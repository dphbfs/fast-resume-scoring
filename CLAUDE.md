# fast-resume-tailoring

## Goal

A Go-based tool that uses Jev to extract and prioritize the Requirements in a
Job Description (Requirement Extractor, done), then link a Resume's Evidence
Units to those Requirements and report each Requirement's Coverage (Resume
Checker, current milestone). No importance ranking, rewriting, generation,
or caching in the Resume Checker milestone.

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
    { "id": "req_1", "value": "Kubernetes", "refs": ["s3", "s9"], "tier": "required", "importance": 0.82 },
    { "id": "req_2", "value": "Go", "refs": ["s5"], "tier": "required", "importance": 0.74 },
    { "id": "req_3", "value": "Ruby", "refs": ["s5"], "tier": "required", "importance": 0.74 }
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
at most one Alternative Group; the Resume Checker gives a group its best
member's Coverage.

The extractor Result also carries `tier` per Requirement (`required |
preferred | mentioned`, same values as golden labels), derived in code from
the strongest Section among its Context Sentences: required > preferred >
responsibilities (-> `mentioned`). Tier is independent of Importance.

## Resume Checker (current milestone)

Goal: given extractor Requirements and a plain-text Resume, produce an
evaluated many-to-many set of Evidence Links and per-Requirement Coverage.
Terms in `CONTEXT.md`; design rationale in `docs/adr/0001`.

1. Resume parsing (code only). Markdown convention:
   `# <Resume Section>`, `## <Role> | <Company or Project> | <dates>`, bullets
   `-` / `•` / `*` (wrapped lines joined). Every Resume Section yields
   Evidence Units, kept verbatim: one per bullet, per prose sentence, per
   Skills line, per Education/Certification entry. Non-conforming input
   parses with blank metadata.
2. Retrieval Round (default `narrow`): a Choice over all Requirements +
   `none` (`none` is a sink only; option description = Requirement + its
   shortest Context Sentence), repeated over the best
   `CHECKER_NARROW_SIZES` (16, then 8) of the previous round; keep the top
   K=8 with p >= 0.01. Other `CHECKER_RETRIEVAL_MODE`s: `single` (one
   Choice), `peel` (take the winner, remove it, ask again), `noul` (one
   yes/no per Requirement). Nothing is linked yet.
3. Strength Round: one Jev request per Evidence Unit with, per retrieved
   Requirement, a gate Noul and a grading Choice (TypeSafe's "Choice
   grades, Noul decides whether" pattern):
   - Gate (`CHECKER_GATE_THRESHOLD` 0.5, wording `v2`): "is `statement`
     evidence the candidate has this requirement?" Yes = work with it, a
     specific instance, a part/prerequisite, or its broader practice, or
     named as their own skill/degree/certificate. Links when P >= 0.5.
   - Grading Choice (`CHECKER_STRENGTH_CRITERIA` `v5`): options are
     `{what, not_for, examples}` objects: strong (the Requirement itself
     is what the work was done with or on; degree/certificate entries are
     strong for what they name), partial (a part, prerequisite, broader
     practice, or minor use), weak (named/listed/claimed only), plus
     non-evidence options `none`, `alternative_tool`,
     `shared_words_only`, `different_skill`, `context_only`. Strength =
     argmax of strong/partial/weak. Examples never come from eval
     fixtures.
   - Skills and Summary units are capped at weak in code. Gate off
     (`CHECKER_GATE_THRESHOLD=0`) falls back to P(strong+partial+weak) >=
     `CHECKER_MIN_EVIDENCE_MASS`. `CHECKER_STRENGTH_MODE=score` (3-level
     Score instead of the Choice) was worse; kept for experiments.
4. Coverage: best Evidence Strength per Requirement, or `none` (flagged,
   never invented). Alternative Group Coverage = best member's Coverage. No
   counts, no Importance use. No years-qualifier special handling. Job
   Summary not used.
   Fit Score (`docs/adr/0002`, code only, `domain.ScoreFit`): Tier-weighted
   average of Coverage credit (strong 1, partial 0.6, weak 0.3; required 3,
   preferred 1.5, mentioned 1), an Alternative Group counting once; plus a
   score per Tier and the Gaps (required items with none). Null when there
   is nothing to score.
5. CLI: `cmd/check -requirements <result.json> -resume <resume.md>
   [-debug trace.json]`. Output (coverage schema v1):

```json
{
  "schema_version": "1", "model": "jev-1.13.0",
  "requirements": [
    { "id": "req_1", "value": "Kubernetes", "tier": "required",
      "coverage": "partial",
      "evidence": [ { "unit": "e4", "strength": "partial", "p": 0.71 } ] }
  ],
  "alternative_groups": [ { "id": "alt_1", "members": ["req_2", "req_3"], "coverage": "strong" } ],
  "fit": { "score": 57, "by_tier": { "required": 58, "preferred": 50 }, "gaps": ["req_5"] },
  "evidence_units": { "e4": { "text": "...", "resume_section": "experience",
      "role": "...", "company": "...", "dates": "2021-03 – 2024-06" } }
}
```

Same non-negotiables as the extractor: slog per stage, metrics, the bounded
Jev limiter, and a trace logging every retrieved pair, its probabilities,
and the accept/reject decision.

### Resume Checker eval

- Fixture = (golden Job Description, Resume) pair; 10-12 pairs:
  synthetic Resumes written against golden Job Descriptions (mixing strong,
  partial, weak, missing, and decoy cases) plus the user's real Resumes,
  each paired with 2 golden Job Descriptions.
- Input Requirements are the golden labels only (V1). Their option
  description is the first Job Description sentence containing the value or
  an alias, found by code; none when no hit.
- Files: `testdata/resumes/<id>.md`, `testdata/checker/<pair>.expected.json`
  listing, per golden Requirement, supporting units (quoted text snippet,
  not a parser ID) with strength; unlisted pairs are none. Years-qualifier
  Requirements are `acceptable` (not scored). Claude drafts, the user
  corrects.
- `cmd/eval -checker` reports retrieval recall, Evidence Link
  precision/recall, strength accuracy (exact and off-by-one), and Coverage
  accuracy per Requirement split by Tier (the main number), and Fit Score
  error (predicted vs labeled Coverage's Fit Score per pair, mean and max).
- Identical runs vary by up to ~0.5 points; compare configs on 3+ runs
  each (mean and range), never on one.
- `make eval-checker` runs it live into `eval/reports/checker/`;
  `eval -checker -rescore <report.json>` rescores offline after label edits.
  Labels: `testdata/checker/README.md`. Tuning log: `docs/tuning.md`
  ("Resume Checker experiment log"). Tasks: `docs/resume-checker-tasks.md`.

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
- Eval caches generated Job Summaries in `eval/cache/summaries` (keyed by
  prompt hash; commit it) so Validation inputs are fixed across runs. Jev
  itself varies by ~0.5 points between identical runs; see `docs/tuning.md`
  for the experiment rules.
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

- Job Descriptions come from the the generative scorer MCP server (`job-tracker`,
  local-scope config); strip recruiter names and emails.
- The user's own Resumes may be pulled from the generative scorer only to build
  Resume Checker fixtures, and must be masked before committing: fake name,
  contacts, links, companies, and schools; keep dates, technologies, and
  metrics. The user reviews masked Resumes before commit.
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
