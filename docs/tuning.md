# Extractor tuning backlog

Next round of tuning for the Requirement Extractor, derived from the eval of
2026-09-30 (run `2026-09-30T02-04-47Z`, rescored against labels
`63732feae2fb`). Terms follow `CONTEXT.md`; eval semantics are in
`testdata/golden/README.md`.

## Where we are

| Metric | Value |
|---|---|
| Recall, loose (strict) | 93.6% (79.6%) |
| Recall: required / preferred / mentioned | 93.9% / 94.3% / 92.1% |
| Precision | 87.5% |
| F1 | 90.4% |
| Expected / predicted | 668 / 1082 |
| Acceptable (not scored) | 276 |
| Duplicates (not scored) | 92 |
| Extras (false positives) | 78 |
| Filler extracted | 11 |
| Misses | 43 |
| Importance tier order / Alternative Group F1 | n/a in the rescore (see step 1) |

## Rules for this round

- Change one thing per experiment, then run `make eval` and compare only
  reports with the same labels fingerprint.
- Keep a change only if F1 improves without recall dropping more than about
  1 point; runs vary by ~1 point because the Job Summary is regenerated.
- Record every experiment (kept or not) in the log at the bottom.
- Label edits are not tuning: fix labels only when they break the README
  rules, then `eval -rescore` and note the new fingerprint.

## Step 1: measure before tuning

1. **Implement `--debug` output** (pipeline stage 6, the next planned task).
   Write, per run, the dropped sentences with their Section, every Chunk with
   its options and the selected option's probability, Refinement answers
   (Filler kind, duplicate/alternative links, Importance score), and merges.
   Without it, a miss cannot be attributed to Section labeling, Validation,
   or Refinement. Store it in eval reports too.
2. **Fresh live baseline** with the reviewed labels, so Importance tier order
   and Alternative Group F1 are measured again (the rescore lost them; the
   last live values, on the old labels, were 83.4% and 63.0%).

## Step 2: code-only fixes (deterministic, no Jev changes)

| # | Problem (count) | Examples | Proposed fix | Stage |
|---|---|---|---|---|
| C1 | Job title fragments extracted as Requirements (7 Filler hits) | "Senior Backend Engineer", "Software Engineer PHP" | Drop a Requirement whose normalized value equals, or is contained in, the job title | Refinement / result |
| C2 | Slash-joined selection hides a Requirement (~8 misses) | "TypeScript/Node.js", "terraform/terragrunt", "OpenSSL/AWS-LC", "microservices/serverless" | When the selected Candidate is slash-joined and each part is also a Candidate of the Chunk, emit each part | Validation |
| C3 | Inline "Label:" prefixes read as Requirements (2 extras) | "Backend Expertise", "Architectural Judgment" (from "Deep Backend Expertise: …") | Strip a leading "Title Case words:" prefix of up to 4 words before chunking | Candidate generation |
| C4 | Years-qualifier fragments (~10 extras) | "years experience", "years of experience working", "5+ years building", "related field", "foreign equivalent" | Add "years", "year", "related", "field", "equivalent", "foreign" to the generic-only words; never offer a Candidate that starts with "years" | Candidate generation |

## Step 3: Jev question tuning (Validation Round)

| # | Problem (count) | Examples | Proposed change |
|---|---|---|---|
| V1 | Years qualifier dropped from the selection (5 misses) | "8+ years of software engineering experience" selected as "software engineering experience" | Add to the question: keep a years qualifier ("5+ years of …") with its skill |
| V2 | Over-long whole-Chunk selections (~9 extras, some misses) | "REST APIs that power the tag-api platform", "Experience building platform-level infrastructure consumed by multiple teams" | Ask for the shortest option that names the requirement completely; try `maxWholeChunkWords` 6 instead of 8 |
| V3 | Single generic words accepted (~35 extras) | "automation", "infrastructure", "security", "cloud", "performance", "standards" | Add a rejection option for "a single broad word that needs more words to name a skill"; alternatively raise `PIPELINE_MIN_REQUIREMENT_MASS` for single lowercase words only |
| V4 | Negated asks extracted (2 Filler hits) | "maintaining legacy systems" ("you're not maintaining…"), "cryptographer" ("you do not need to be…") | Add a `negated` rejection option: the sentence says the applicant does not need or will not do it |

## Step 4: Refinement Round tuning

| # | Problem (count) | Examples | Proposed change |
|---|---|---|---|
| R1 | Unmerged duplicates (92) | "Golang"/"Go", "Kubernetes clusters"/"Kubernetes", "logs"/"metrics"/"alarms" under observability, "hardware security modules"/"HSMs" | Offer padded variants of the same sentence as duplicate options; consider merging when one value's words are a strict subset of another's in the same sentence and both are selected from one list |
| R2 | Perks extracted as Requirements (3 Filler hits) | "GPT-Codex 5/3", "Claude Opus 4.6", "Gemini 3 Pro" | Add a `perk_or_benefit` Filler option |
| R3 | Category Requirements missing (~8 misses) | "backend programming", "systems programming", "programming languages", "platform-level infrastructure" | First attribute with `--debug` (Validation vs Filler drop); if Filler drops them, add "a named category of skill (backend programming)" to the keep description |
| R4 | List items missing (~10 misses) | "sales", "finance", "ISRG", "Redis", "Django", "detection systems" | Attribute with `--debug` first; likely single-word Chunks rejected as generic or dropped as vague |

## Step 5: re-check the eval itself

- **Acceptable is large (276).** Skim it per fixture; anything that should
  be a clear error moves to `filler`, anything required moves to
  `requirements`.
- **Pooling bias.** Missing labels were found from the extractor's own
  extras. Label 3-5 fresh postings from `testdata/jd/` without looking at
  extractor output, then compare recall on them with the golden set.

## Experiment log

| Date | Change | Labels | Recall (strict) | Precision | F1 | Kept |
|---|---|---|---|---|---|---|
| 2026-09-30 | Baseline after label review (rescore) | `63732feae2fb` | 93.6% (79.6%) | 87.5% | 90.4% | n/a |
