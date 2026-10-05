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
- **Noise.** Eval caches Job Summaries (`eval/cache/summaries`), so inputs
  are fixed across runs, but Jev itself is not fully deterministic: with
  identical inputs, 1.5% of Section labels, 3.6% of Chunk selections and
  6.7% of Refinement decisions flipped between two runs (near-ties), moving
  the headline by ~0.5 points. Differences under ~1 point are noise.
- Decide small changes by **mechanism plus numbers**: the trace must show
  the targeted misses/extras going away, and the headline must not get worse
  beyond noise. For effects near the noise, run each variant twice.
- Keep a change only if F1 improves (or ties within noise with its mechanism
  verified) without recall dropping more than about 1 point.
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
| ~~C1~~ done | Job title fragments extracted as Requirements (7 Filler hits) | "Senior Backend Engineer", "Software Engineer PHP" | Drop a Requirement whose normalized value equals, or is contained in, the job title | Refinement / result |
| ~~C2~~ done | Slash-joined selection hides a Requirement (~8 misses) | "TypeScript/Node.js", "terraform/terragrunt", "OpenSSL/AWS-LC", "microservices/serverless" | When the selected Candidate is slash-joined and each part is also a Candidate of the Chunk, emit each part | Validation |
| ~~C3~~ done | Inline "Label:" prefixes read as Requirements (2 extras) | "Backend Expertise", "Architectural Judgment" (from "Deep Backend Expertise: …") | Strip a leading "Title Case words:" prefix of up to 4 words before chunking | Candidate generation |
| ~~C4~~ done | Years-qualifier fragments (~10 extras) | "years experience", "years of experience working", "5+ years building", "related field", "foreign equivalent" | Add "years", "year", "related", "field", "equivalent", "foreign" to the generic-only words; never offer a Candidate that starts with "years" | Candidate generation |
| ~~C5~~ done | Short unpunctuated bullet lines skipped as headings (3 misses) | "Experience with TypeScript/Node.js", "Designing new microservices or systems" | Only skip a sentence as a heading when it ends with ":" or is a markdown heading; keep the short-line rule for heading context only | Candidate generation |

### Finding: the Job Summary matters

Runs with the fallback summary (title + required/preferred sentences; used
when the generative endpoint is unreachable) score ~4 points lower
precision (~82.5%) than the run with a generated summary (86.8%), at equal
recall. Keep the generative summary; make sure the endpoint is reachable
before a real (non-fixed-input) eval, and compare fallback runs only with
fallback runs.

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
| 2026-09-30 | C2 first run (live summaries): scoring misses 7 -> 3, but headline fell; other stages moved too, which exposed run noise | `63732feae2fb` | 93.3% (79.5%) | 83.4% | 88.1% | re-tested below |
| 2026-09-30 | Fixed inputs (fallback summaries, endpoint down): baseline (no C5, no C2) | `63732feae2fb` | 91.9% (76.6%) | 82.6% | 87.0% | reference |
| 2026-09-30 | Fixed inputs: C5 | `63732feae2fb` | 92.5% (76.5%) | 81.6% | 86.7% | kept (within noise, mechanism verified) |
| 2026-09-30 | Fixed inputs: C5 + C2, two runs | `63732feae2fb` | 93.3% / 93.0% | 82.5% / 82.0% | 87.6% / 87.2% | yes (+0.7 F1 vs C5, both runs) |
| 2026-09-30 | C1: drop job-title fragments before Refinement. Offline on both C5 + C2 runs: removes 4 / 5 predictions, all Filler hits, no matches | `63732feae2fb` | unchanged | +~0.4 | +~0.2 | yes (exact, deterministic filter) |
| 2026-09-30 | C3 + C4 (label prefixes stripped; years/qualification fragments not offered), two fixed-input runs with C1-C5. Trace: label-prefix extras 1 -> 0, years/qualification extras 7-8 -> 0-1 | `63732feae2fb` | 93.0% / 93.0% | 82.7% / 82.9% | 87.5% / 87.6% | yes (mechanisms verified, +~0.5 precision, Filler 8-9 -> 3-6 incl. C1) |
| 2026-10-01 | Tier added to Requirements (code rule: strongest Section wins). Offline rescore of the 2026-09-30T02-46-18Z run, Tier derived from stored Context: accuracy required 96.0% (314/327), preferred 73.8% (107/145), mentioned 88.7% (134/151) | `63732feae2fb` | unchanged | unchanged | unchanged | yes (preferred is the weak spot; revisit only if Resume Checker eval needs it) |

## Resume Checker experiment log

Run with `make eval-checker` (11 pairs in `testdata/checker`). Labels are
Claude's first draft, not yet reviewed by the user; real-resume labels are
known to be under-inclusive (many "false links" are valid evidence), so
link precision is understated. Retrieval recall is the most trustworthy
number until the review.

| Date | Change | Labels | Coverage | Retrieval recall (S+P) | Link P / R | Strength exact | Report |
|---|---|---|---|---|---|---|---|
| 2026-10-01 | Baseline: K 5, floor 0.02, mass 0.5 | `25ff7643d0b3` | 81.1% | 83.8% (80.5%) | 57.5% / 83.8% | 85.9% | `2026-10-01T01-26-55Z` |
| 2026-10-01 | K 8 | `25ff7643d0b3` | 81.1% | 83.8% | 56.0% / 83.8% | 85.9% | `2026-10-01T01-27-26Z` |
| 2026-10-01 | K 12 | `25ff7643d0b3` | 81.8% | 83.5% | 55.9% / 83.5% | 86.9% | `2026-10-01T01-27-36Z` |
| 2026-10-01 | K 8, floor 0.005 | `25ff7643d0b3` | 81.8% | 90.7% | 49.9% / 90.3% | 85.9% | `2026-10-01T01-27-53Z` |
| 2026-10-01 | K 8, floor 0.001 | `25ff7643d0b3` | 81.1% | 89.4% | 50.2% / 89.1% | 86.0% | `2026-10-01T01-28-04Z` |

Findings:

- Link recall equals retrieval recall in every run: the Strength Round
  links nearly every labeled pair it sees, so retrieval is the recall
  bottleneck.
- K is not the limit; the floor is. The retrieval Choice concentrates mass
  on one Requirement, so a bullet that supports 6+ Requirements (e.g. the
  real-backend analytics-pipeline bullet) leaves the others below 0.02.
  Even at floor 0.005 recall is 90.7%, under the ADR 0001 trigger (95%):
  next experiment is Noul retrieval (one independent yes/no per
  Requirement).
- Strength: the model rates labeled-partial pairs strong 20 times
  (confusion row partial: 20 strong / 19 partial). Either the partial
  definition needs sharper contrasts or the labels are strict; decide
  after the label review.

### 2026-10-01: label gaps, negative options, multi-round retrieval

Labels: 163 links added from the baseline's false links (broad
Requirements now labeled on every supporting bullet), and a policy fix: a
competing tool of the same kind is not evidence (Docker Swarm for
Kubernetes removed). New labels fingerprint after the fix; the old rows
above are not comparable. Pooling bias: the added links came from
single-mode predictions, so link precision is inflated for single mode and
understated for modes that find new pairs.

Noise: identical runs vary by up to ~0.5 points on every metric. Each row
below is the mean [min-max] of 3-4 runs.

| Config | Runs | Coverage | Retrieval R | Link P | Link R | Strength exact | Jev $/run |
|---|---|---|---|---|---|---|---|
| v1 criteria, single | 4 | 81.3 [80.8-81.5] | 88.0 | 90.4 | 88.0 | 84.4 | 0.022 |
| v2 (sharpened strong/partial), single | 1 | 81.8 | 87.8 | 87.8 | 87.8 | 84.7 | |
| **v3 (v2 + negatives), single (new default)** | 4 | **84.7 [84.6-85.0]** | 88.0 | 92.9 | 86.9 | 86.6 | 0.027 |
| v3, peel K 6 (no shortlist) | 1 | 82.5 | 91.3 | 73.9 | 90.1 | 86.0 | |
| v3, peel K 6, shortlist 12 | 4 | 83.9 [83.6-84.3] | 90.6 | 79.0 | 89.5 | 85.9 | 0.057 |
| v3, narrow 12,4, K 6 | 1 | 81.8 | 82.2 | 92.0 | 81.0 | 87.7 | |
| v3, narrow 16,8, K 8, floor 0.01 | 4 | 84.4 [83.9-85.0] | 91.6 | 85.7 | 90.3 | 86.7 | 0.044 |
| same, mass 0.65 | 3 | 85.1 [85.0-85.3] | 91.6 | 87.6 | 88.4 | 87.0 | |
| same, mass 0.8 | 3 | 85.4 [85.0-85.7] | 91.8 | 90.8 | 83.3 | 86.9 | |
| v3, single, mass 0.65 | 3 | 83.9 [83.2-84.3] | 87.9 | 94.3 | 84.9 | 86.5 | |

Findings:

- Negative options (v3) are the clear win: +3.4 Coverage over v1, ranges
  do not overlap, link precision up, required-tier Coverage 79.1 -> 85.0.
  Sharpening strong/partial alone (v2) did nothing measurable.
- Multi-round retrieval works as designed: peel and narrow raise retrieval
  recall by 2.6-3.8 points (the softmax-splitting problem). But the extra
  pairs are mostly borderline, and v3 still links many of them, so Coverage
  does not rise unless the evidence-mass threshold rises with it (narrow +
  mass 0.65-0.8: +0.4 to +0.7 over v3 single, at 1.6x cost). Within the
  label-bias margin: re-test after the user's label review before making
  narrow the default.
- Peel without a shortlist is the worst: each extra round over ~40 options
  lets the model pick a loosely related Requirement instead of none.
- Retrieval recall still tops out near 92% (ADR 0001 trigger: 95%).
  Noul retrieval is still untested.
- Remaining stable false links that v3 lets through, for the next negative
  option: inferring a Requirement from a capability the work would need
  (API authentication <- "integrations with AWS, Azure"; OAuth2 <- "auth
  libraries"; SQL <- "maintained the financial report").

### 2026-10-01: Noul retrieval and the needed_capability negative

Same labels as the previous section. Mean [min-max] of 3 runs each, plus a
fresh v3 control in the same session.

| Config | Coverage | Retrieval R | Link P | Link R | Strength exact | Required Cov | Jev $/run |
|---|---|---|---|---|---|---|---|
| v3, single (control) | 84.3 [84.3-84.3] | 88.0 | 92.3 | 86.9 | 86.3 | 85.1 | 0.027 |
| v4 (v3 + needed_capability), single | 83.9 [83.6-84.3] | 87.7 | 94.1 | 85.0 | 84.2 | 83.4 | 0.028 |
| v3, noul K 8, threshold 0.5 | 83.1 [82.5-83.6] | **97.0** | 62.2 | 96.0 | 87.0 | 84.6 | 0.061 |
| v3, noul K 8, threshold 0.3 | 79.8 [79.7-80.1] | 97.9 | 58.4 | 96.3 | 86.4 | 83.2 | 0.066 |
| v4, noul 0.5 | 81.5 [80.8-82.5] | 97.1 | 65.1 | 94.1 | 83.2 | 83.7 | 0.063 |
| v3, noul 0.5, mass 0.8 | 83.2 [83.2-83.2] | 96.9 | 71.7 | 88.8 | 86.2 | 83.4 | 0.061 |
| v4, noul 0.5, mass 0.8 | 81.8 [81.5-82.5] | 96.8 | 77.8 | 80.6 | 82.9 | 80.7 | 0.063 |
| **v3, noul 0.7, mass 0.8** | **85.1 [85.0-85.3]** | 90.8 | 76.8 | 86.5 | 86.7 | 84.1 | 0.056 |

Findings:

- Noul retrieval fixes retrieval recall (97.0%, past the ADR 0001 95%
  trigger): independent yes/no answers do not split mass the way one
  Choice does. But it hands the Strength Round ~3x more borderline pairs,
  and v3 links too many (link precision 62%), so Coverage falls.
- A sample of 45 stable noul-only false links: ~1 in 4 are label gaps
  (compiled language <- "shipped an iOS app in Swift"), ~3 in 4 real false
  positives (Kotlin <- "Android apps in Java"; customer-facing <- "designed
  backend services"). The precision drop is mostly real.
- Best Coverage so far: noul 0.7 + mass 0.8 at 85.1, +0.8 over v3 single
  with non-overlapping ranges, at 2x cost; required-tier Coverage is not
  better (84.1 vs 85.1). Not adopted: the gain is small, and the Strength
  Round, not retrieval, now limits Coverage.
- needed_capability (v4) raises link precision but costs recall and strength
  accuracy (it takes probability from partial); no gain in any
  combination. Kept selectable, not default.
- Next lever is the Strength Round's precision on borderline pairs (most
  surviving false positives are partial links), not more retrieval.

### 2026-10-01: TypeSafe guidance review, then A (structured options), B (gate Noul), C (Score grading)

Guidance from docs.typesafe.ai (Jev 1.13 jaggedness, building guide,
primitives, structure, confidence, composite scoring, skill-suggestion and
citation-check cookbooks) that applies here:

- One judgment per question; split mixed judgments and combine in code.
  Our Strength Choice mixed "is it evidence", "why not", and "how strong".
- Ordinal judgments ("how strong") suit a Score with concrete levels.
- Choice options can be `{what, not_for, examples}` objects; `not_for`
  sharpens boundaries.
- Rank with a Choice, decide "whether" with an absolute Noul per candidate
  (skill-suggestion cookbook). Our earlier Noul retrieval used Nouls as the
  ranker over all Requirements, which the docs do not recommend.
- Literal reading: align instructions and criteria; when explaining a
  wrong answer, that explanation is the missing instruction.
- Choice and Noul thresholds are not interchangeable.

Results, mean [min-max] of 3 runs each, same labels as the sections above:

| Config | Coverage | Retrieval R | Link P | Link R | Strength exact | Required Cov | Jev $/run |
|---|---|---|---|---|---|---|---|
| v3, single (control) | 83.9 [83.6-84.3] | 88.3 | 92.5 | 87.2 | 86.5 | 83.9 | 0.027 |
| A: v5 (structured options), single | 84.8 [84.6-85.0] | 87.8 | 93.0 | 86.4 | 86.6 | 85.1 | 0.033 |
| B: v3 + gate 0.5, single | 83.0 [82.5-83.6] | 88.5 | 98.0 | 77.2 | 85.8 | 81.4 | 0.030 |
| A+B: v5 + gate 0.5, single | 83.6 [83.6-83.6] | 88.1 | 97.7 | 76.9 | 85.3 | 81.6 | 0.036 |
| v3 + gate 0.5, narrow 16,8 | 84.6 [84.6-84.6] | 91.8 | 93.8 | 79.9 | 85.8 | 83.4 | 0.048 |
| v3 + gate 0.5, noul retrieval | 84.6 [84.3-85.0] | 96.8 | 80.2 | 85.6 | 85.5 | 83.9 | 0.066 |
| v3 + gate 0.4, noul retrieval | 86.0 [85.3-86.4] | 97.0 | 75.3 | 91.6 | 86.4 | 85.5 | 0.066 |
| v5 + gate 0.4, noul retrieval | 86.4 [86.0-86.7] | 97.0 | 75.2 | 91.3 | 85.2 | 86.4 | 0.076 |
| v5 + gate 0.4 (v1 wording), narrow 16,8 | 86.6 [86.4-87.1] | 91.9 | 90.9 | 85.7 | 85.7 | 86.4 | 0.055 |
| **v5 + gate 0.5 (v2 wording), narrow 16,8 (new default)** | **86.9 [86.7-87.4]** | 91.6 | 90.8 | 84.7 | 85.7 | **87.6** | 0.055 |
| C: Score grading + gate 0.4 v2, narrow | 82.6 [82.5-82.9] | 91.4 | 88.6 | 87.4 | 80.3 | 83.2 | 0.042 |
| C: Score grading + gate 0.4 v1, narrow | 84.3 [83.9-84.6] | 91.6 | 91.3 | 85.2 | 79.5 | 82.8 | 0.041 |

Gate thresholds were swept offline from traces (gate P is recorded per
pair; the re-scorer reproduced live numbers exactly at the run's own
threshold). v1 wording peaked at 0.4 in all four retrieval configs; v2
wording peaked at 0.5 (offline 87.5, live 86.9: the offline gain shrank,
a sign threshold picking on 11 pairs is near its limit).

Findings:

- A (structured `{what, not_for, examples}` options) is a small, real
  gain (+0.9, ranges do not overlap); the same boundaries as prose (v2)
  did nothing.
- B (gate Noul deciding links) is what makes high-recall retrieval pay
  off: with narrow or noul retrieval it removes all false links on
  Requirements with no evidence (label none -> predicted partial: 5.3 per
  run -> 0). Coverage +2.7 over the control overall, +3.7 on required.
- The v1 gate wording ("the requirement itself") contradicted the partial
  criterion and dropped partial pairs; v2 includes part/prerequisite/
  broader practice. Equal Coverage, +1.2 on required; adopted because it
  removes the contradiction.
- C (3-level Score instead of the grading Choice) is worse: strength
  accuracy falls ~6 points. The Choice with structured options and
  negatives grades better than ordered levels here. Not adopted.
- Remaining errors are almost all in the partial row: ~15 labeled-partial
  pairs graded strong, ~14 dropped by the gate. Partial is also the most
  subjective label; review it before tuning further.
- Overfitting risk: thresholds and options have now been picked on these
  11 pairs. Before the next round, add fresh pairs (or hold out a few) to
  confirm the gains.

### Fit Score (2026-10-02)

- Fit Score added (ADR 0002) with Fit Score error in the report: the Fit
  Score of predicted vs labeled Coverage per pair, over non-Skip
  Requirements. Offline rescore of `02-16-53Z`
  (`2026-10-02T01-23-18Z-rescored`): mean 3.9, max 11 points. Mostly low
  (partial predicted none); worst on real-backend × Experimentation (66 vs
  77) and syn-android × Android (61 vs 70). Pair ranking mostly holds.
  Years qualifiers are Skip in eval, so their effect is not measured here.

### Skills line split (2026-10-02)

- Bug: retrieval keeps at most K=8 Requirements per Evidence Unit, so the
  real 26-item "Technical Stack" line could support only 8. Found on 10
  live applications: TypeScript, Redis, GitHub Actions were Gaps though
  listed. Fix: the parser cuts Skills lines with > 6 items into balanced
  chunks that repeat the label; real-backend labels remapped to the chunk
  holding each item (data-analyst labels unchanged).
- Eval, 3 runs (`2026-10-02T01-39-37Z`, `01-40-08Z`, `01-40-44Z`):
  Coverage 86.4% (85.7-87.1) vs 86.9% baseline, link P ~88.8% (was ~91),
  Fit Score error 3.8-3.9. Flat: in the fixtures, skills-line
  Requirements almost always also have stronger bullet evidence.
- 10 live applications, same extracted Requirements, check only: 17
  Requirements moved from none to linked (mostly weak skills hits:
  TypeScript, Redis, GitHub Actions, NoSQL, ECS); Gaps 69 -> 62; Everest
  36 -> 39, Automox 56 -> 58; other Fit Scores within +-3 (run noise).
- Seen in passing, separate issue: gate 0.69 linked "Built a Golang CLI"
  to Node.js as strong while the grading Choice put 0.73 on
  `alternative_tool` (evidence mass 0.26). Gate and grader disagree;
  consider rejecting when a non-evidence option wins the grading Choice.

### Gate vs grader: competing-tool links (2026-10-02)

- Offline simulation on the traces of the 3 skills-split runs (unlink a
  gate-passed pair when the grading Choice puts >= t on a non-evidence
  option): at t 0.5-0.7 only 0-3 pairs per 3 runs are vetoed, Coverage
  86.2-86.4 vs 86.4; "veto when a negative is the top option" vetoes
  8-16 pairs, mostly correct ones (Coverage 85.8). The eval set barely
  contains this error.
- On the 10 live applications the same veto would also remove correct
  links: AWS for "major cloud platform" (alternative_tool 0.77) and
  Prometheus/Grafana for "observability"/"monitoring" (0.76-0.86) sit in
  the same range as the real error (Golang CLI for Node.js, 0.73-0.82).
  The grader reads "an instance of a broad Requirement" as a competing
  tool, so no threshold separates them.
- Built, not yet measured: criteria `v6` (v5 plus an alternative_tool
  `not_for`: an instance of a broader requirement is the requirement
  itself) and `CHECKER_VETO_THRESHOLD` (default 0 = off). Plan: 3 runs each
  of v6 and v6 + veto 0.6, plus the 10 applications to check that AWS and
  Prometheus stop scoring alternative_tool while Golang -> Node.js still
  does. Blocked: OpenRouter key hit its monthly limit (HTTP 403) on the
  first v6 run; partial reports deleted.
- 10 live applications, one run each, same Requirements (v5 / v6 /
  v6 + veto 0.6): Fit vs RR Spearman -0.07 / -0.02 / -0.10. v6 lowers
  alternative_tool on instances only a little (AWS 0.77 -> 0.66-0.72,
  Prometheus for monitoring 0.86 -> 0.73). Veto 0.6 removed 5 links: 4
  correct (observability, monitoring, AWS x2 for "major cloud platform")
  and 1 wrong (NestJS <- Java/Golang skills chunk). Veto rejected at 0.6.
- The Golang CLI -> Node.js link disappeared under v6, but not because of
  v6: the Retrieval Round proposed Node.js for that unit in only 1 of 4
  runs (v5 runs included). The error is rare and retrieval-dependent.
- Eval, v6, 3 runs (`2026-10-02T01-56-55Z`, `01-57-21Z`, `01-57-50Z`) vs
  v5 (skills-split runs above): Coverage 86.6% (86.0-87.1) vs 86.4%
  (85.7-87.1); required 86.9% vs 86.7%; link P 88.4% vs 88.8%; Fit Score
  error 3.8 vs 3.8; alternative_tool rejections 14-15 vs 15-17. No
  difference beyond noise. Default stays v5; v6 kept as an option.

Reports kept in `eval/reports/checker/`: the five first-round runs cited
above, and the three runs of the current default (`2026-10-01T02-15-52Z`,
`02-16-24Z`, `02-16-53Z`) as the reference baseline. The other ~90
experiment reports were pruned; their numbers are in the tables above.

### Generative baseline (2026-10-02, branch `exp/generative-baseline`)

Question: is the Jev Resume Checker a cheaper replacement for the
traditional one-prompt generative match score, at the same accuracy?
`eval -checker -baseline` also scores each pair with Reactive Resume's
match-score prompt (`packages/api/src/features/applications/ai.ts`,
verbatim, Resume as markdown) through `OPENAI_*`, uncached, and
compares both against the Fit Score of the labeled Coverage.
`-baseline-price-in/-out` (USD per M tokens) price the calls; with
`-rescore` they reprice stored runs.

3 runs (`2026-10-02T02-28-38Z`, `02-29-42Z`, `02-31-42Z`; priced in the
`-rescored` reports), `claude-opus-5` at $5 / $25 per M tokens:

| | Jev (Resume Checker) | Generative |
|---|---|---|
| Fit error vs labeled, mean (range) | 4.0 (3.6-4.3) | 6.4 (5.5-7.3) |
| Fit error max | 9-13 | 22-26 |
| Fit bias | -1.8 to -2.5 | -1.5 to 0.0 |
| Time per pair, mean | 6.3s | 16.6s (13.4-19.7) |
| Cost per pair | $0.0054 | $0.029 |

- Jev is ~5.4× cheaper per pair for the Resume Checker alone. Adding
  the Requirement Extractor (~$0.0056, once per Job Description) gives
  ~$0.011 when each posting is scored against one Resume: ~2.7×
  cheaper. Generative output tokens are ~3/4 of its cost (~840 out).
- Generative input tokens are estimated (prompt chars / 4): the local
  proxy reports `prompt_tokens: 2` for every prompt.
- The largest generative misses are domain-heavy postings it rates
  holistically (Golang security integrations: labeled 36, generative
  62). Caveat: the target is our own labeled Coverage under the Fit
  Score formula, so this measures agreement with the Coverage view of
  fit, which favors Jev by construction. Real outcomes (interview vs
  rejection) are still the only neutral target.
- Coverage in these runs: 86.7-87.1%, in line with the reference runs.

### Cheaper Jev: skip unused questions (2026-10-02, branch `exp/cheaper-jev`)

Jev bills input tokens only (~$0.042 per M; every report fits with output
at $0). Default split: retrieval 53%, strength 47%. On 4 runs' traces,
half of the grading Choices went unused (29% Skills/Summary pairs, capped
at weak; 28% gate-rejected), and the third narrow round changed 1 of 1786
labeled retrieved pairs. Options built (opt-in):
`CHECKER_SKIP_CAPPED_GRADING`, `CHECKER_GATE_FIRST`,
`CHECKER_NARROW_STOP_P`; plus `CHECKER_NARROW_SIZES=16`.

3 runs each (default: 4 runs, `02-28-38Z`..`02-49-03Z`):

| Config | Coverage | Required | Link P / R | Fit err | $/pair | ms/pair |
|---|---|---|---|---|---|---|
| default | 86.8 (86.4-87.1) | 87.4 | 88.9 / 85.4 | 4.0 | 0.0053 | 7.4s |
| A skip capped | 86.7 (86.4-87.1) | 87.4 | 88.5 / 86.0 | 3.8 | 0.0048 | 8.4s |
| B A + gate first | 87.3 (86.7-87.8) | 87.8 | 88.9 / 85.2 | 3.9 | 0.0046 | 9.7s |
| C B + narrow 16 | 87.4 (87.1-87.8) | 88.0 | 88.2 / 86.8 | 3.2 | 0.0041 | 7.4s |
| D B + stop 0.99 | 86.7 (86.4-87.1) | 87.8 | 88.8 / 85.4 | 4.1 | 0.0043 | 9.0s |
| E all four | 87.2 (86.7-87.8) | 88.0 | 88.3 / 86.4 | 3.9 | 0.0039 | 8.2s |

- No accuracy loss in any config: every Coverage range overlaps the
  default's. Narrow 16 trades ~0.6 link precision for ~1.4 link recall
  (more pairs reach Strength), and the gate still filters them.
- E is the cheapest: -26% ($0.0053 -> $0.0039 per pair), about 7.4x
  cheaper than the generative baseline ($0.029). C is -23% with default
  latency.
- Gate first saved less than estimated (-4% vs ~-8%): its second request
  resends the state, and gates are a larger share of strength than the
  rubric-size estimate assumed. It adds ~1.3s per pair (two sequential
  requests per unit); dropping the third narrow round wins that back.
- Decision: C is the default (`CHECKER_NARROW_SIZES=16`,
  `CHECKER_SKIP_CAPPED_GRADING=true`, `CHECKER_GATE_FIRST=true`): -23%
  cost, default latency, the best mean Coverage and Fit error (3.2,
  range 3.0-3.6 vs the old default's 3.6-4.3).

### Losing variants deleted (2026-10-04, review v1)

Deleted from the code after Product Review v1: retrieval modes `single`
(now narrow with empty `CHECKER_NARROW_SIZES`), `peel`, `noul`;
`CHECKER_NARROW_STOP_P`; Score grading (`CHECKER_STRENGTH_MODE=score`);
criteria v1-v4 and v6 (v5 is the only rubric); gate wording v1; the veto
(`CHECKER_VETO_THRESHOLD`). Their results stay in the entries above and in
the committed reports; setting a removed env var is a config error.
Directly constructed Checkers now get criteria v5 and gate wording v2
(before: v3 and v1, unlike the env defaults).

### End-to-end baseline vs the generative reference (2026-10-05, review v1 E3)

`make eval-e2e` (report `eval/reports/e2e/2026-10-05T02-35-23Z`): the 30-pair
subset of `testdata/reference`, extraction + checking cold, Fit Score vs
today's generative reference score (`current.json`).

| Metric | Value |
|---|---|
| MAE / bias | 20.5 / -17.5 |
| Within ±5 / ±10 | 13% / 30% |
| Max error | 45 (Stripe high-school fellowship: Fit 53, reference 8) |
| Pearson / Kendall τ-b | 0.60 / 0.46 |
| Jev cost per pair (cold) | $0.0168 (checker ~57% of input tokens) |
| Time per pair | 20.5s (parallel 4) |

- The Fit Score runs ~17 points low; a constant shift alone would give
  MAE ~11.9 in-sample (not a valid estimate, only the size of the bias).
  Ranking is the real gap: τ-b 0.46 vs the reference's own noise of
  ~1 point per call.
- Requirements per posting average 54 (up to 122) against ~30 on the
  golden set: these Job Descriptions carry Hermes's condensed stack
  section plus the full posting, so extraction sees most text twice.
  Error does not correlate with the count (r = -0.15), but cost does.
- The largest misses are holistic judgments the coverage formula cannot
  make: eligibility (a high-school fellowship), and seniority/scope.
- Cost: $0.0168 per pair cold vs ~$0.05 for one Opus 5 call, about 3×
  cheaper; the 5× target needs about $0.010.
