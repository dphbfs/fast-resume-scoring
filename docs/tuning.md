# Extractor tuning backlog

Next round of tuning for the Requirement Extractor, derived from the eval of
2026-09-30 (live run `2026-09-30T02-37-41Z` against labels `63732feae2fb`,
with extraction traces). Terms follow `CONTEXT.md`; eval semantics are in
`testdata/golden/README.md`.

## Where we are

| Metric | Value |
|---|---|
| Recall, loose (strict) | 93.4% (79.2%) |
| Recall: required / preferred / mentioned | 94.2% / 93.0% / 92.1% |
| Precision | 86.8% |
| F1 | 90.0% |
| Expected / predicted | 668 / 1069 |
| Acceptable (not scored) | 260 |
| Duplicates (not scored) | 90 |
| Filler extracted | 8 |
| Misses | 44 |
| Importance tier order | 81.9% |
| Alternative Group F1 | 68.5% |
| Jev cost (20 postings) | $0.11 |

Misses by stage (from the trace; `eval` reports them per miss):

| Stage | Misses | Backlog items |
|---|---|---|
| validation_not_selected | 11 | V1, V2 |
| scoring (slash-joined pick matched one label, hid the other) | 9 | C2 |
| refinement_merged | 8 | R1 |
| refinement_filler | 6 | R3 |
| validation_rejected | 6 | V5 |
| section_dropped | 3 | C5 |
| no_candidate | 1 | - |

## Rules for this round

- Change one thing per experiment, then run `make eval` and compare only
  reports with the same labels fingerprint.
- Keep a change only if F1 improves without recall dropping more than about
  1 point; runs vary by ~1 point because the Job Summary is regenerated.
- Record every experiment (kept or not) in the log at the bottom.
- Label edits are not tuning: fix labels only when they break the README
  rules, then `eval -rescore` and note the new fingerprint.

## Step 1: measure before tuning

1. ~~Implement `--debug` output~~ Done: `extract -debug trace.json`; eval
   writes traces to `eval/reports/<run>-traces/` (gitignored) and attributes
   every miss to a stage.
2. ~~Fresh live baseline~~ Done: the table above.

## Step 2: code-only fixes (deterministic, no Jev changes)

| # | Problem (count) | Examples | Proposed fix | Stage |
|---|---|---|---|---|
| C1 | Job title fragments extracted as Requirements (7 Filler hits) | "Senior Backend Engineer", "Software Engineer PHP" | Drop a Requirement whose normalized value equals, or is contained in, the job title | Refinement / result |
| C2 | Slash-joined selection hides a Requirement (~8 misses) | "TypeScript/Node.js", "terraform/terragrunt", "OpenSSL/AWS-LC", "microservices/serverless" | When the selected Candidate is slash-joined and each part is also a Candidate of the Chunk, emit each part | Validation |
| C3 | Inline "Label:" prefixes read as Requirements (2 extras) | "Backend Expertise", "Architectural Judgment" (from "Deep Backend Expertise: …") | Strip a leading "Title Case words:" prefix of up to 4 words before chunking | Candidate generation |
| C4 | Years-qualifier fragments (~10 extras) | "years experience", "years of experience working", "5+ years building", "related field", "foreign equivalent" | Add "years", "year", "related", "field", "equivalent", "foreign" to the generic-only words; never offer a Candidate that starts with "years" | Candidate generation |
| C5 | Short unpunctuated bullet lines skipped as headings (3 misses) | "Experience with TypeScript/Node.js", "Designing new microservices or systems" | Only skip a sentence as a heading when it ends with ":" or is a markdown heading; keep the short-line rule for heading context only | Candidate generation |

## Step 3: Jev question tuning (Validation Round)

| # | Problem (count) | Examples | Proposed change |
|---|---|---|---|
| V1 | Years qualifier dropped from the selection (5 misses) | "8+ years of software engineering experience" selected as "software engineering experience" | Add to the question: keep a years qualifier ("5+ years of …") with its skill |
| V2 | Over-long whole-Chunk selections (~9 extras, some misses) | "REST APIs that power the tag-api platform", "Experience building platform-level infrastructure consumed by multiple teams" | Ask for the shortest option that names the requirement completely; try `maxWholeChunkWords` 6 instead of 8 |
| V3 | Single generic words accepted (~35 extras) | "automation", "infrastructure", "security", "cloud", "performance", "standards" | Add a rejection option for "a single broad word that needs more words to name a skill"; alternatively raise `PIPELINE_MIN_REQUIREMENT_MASS` for single lowercase words only |
| V4 | Negated asks extracted (2 Filler hits) | "maintaining legacy systems" ("you're not maintaining…"), "cryptographer" ("you do not need to be…") | Add a `negated` rejection option: the sentence says the applicant does not need or will not do it |
| V5 | Chunks rejected just under the threshold (6 misses, mass 0.49-0.66) | "Troubleshooting" (action_only), "finance", "custodians", "DigiCert", "ISRG" (people_or_context) | Say in `people_or_context` that a named product, vendor, or certificate authority the applicant should know is a requirement; then re-sweep `PIPELINE_MIN_REQUIREMENT_MASS` 0.6-0.7 |

## Step 4: Refinement Round tuning

| # | Problem (count) | Examples | Proposed change |
|---|---|---|---|
| R1 | Unmerged duplicates (90) and wrong merges (8 misses) | Unmerged: "Golang"/"Go", "Kubernetes clusters"/"Kubernetes", "hardware security modules"/"HSMs". Wrong: "SDK development" into "SDK", "Vue" into "component-based frameworks", "puppet" into "Configuration management" | Offer padded variants of the same sentence as duplicate options; do not merge a specific item into a category (require similar length/specificity, or ask a broader/narrower question first); pick the more specific value as canonical |
| R2 | Perks extracted as Requirements (3 Filler hits) | "GPT-Codex 5/3", "Claude Opus 4.6", "Gemini 3 Pro" | Add a `perk_or_benefit` Filler option |
| R3 | Category Requirements dropped as `vague_term` (6 misses, confirmed by trace) | "backend programming", "backend engineering skills", "scalability", "high throughput", "full-stack", "similar programming languages" | Add "a named category or quality of engineering work (backend programming, scalability, high throughput)" to the keep description, and move those words out of the vague_term examples |

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
| 2026-09-30 | Live baseline with traces (tier order 81.9%, group F1 68.5%) | `63732feae2fb` | 93.4% (79.2%) | 86.8% | 90.0% | n/a |
| 2026-09-30 | C5: skip only strong headings (":" or markdown); section_dropped 3 -> 0 | `63732feae2fb` | 94.3% (79.6%) | 86.1% | 90.0% | yes (F1 tie, recall +0.9, removes a failure class) |
