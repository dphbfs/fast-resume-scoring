# Project progress (2026-10-02)

The Requirement Extractor is done, and the Resume Checker is built,
evaluated and tuned to 87.4% Coverage accuracy (exact match against the
labeled strength per Requirement) at $0.0041 of Jev per posting–Resume
pair. Against the traditional approach (one generative prompt) it is
more accurate and about 7× cheaper. The next step is the user's review
of the masked resumes and the labels. Terms follow `CONTEXT.md`; experiment
details are in `docs/tuning.md`; open tasks are in
`docs/resume-checker-tasks.md`.

## Requirement Extractor (Job Description → prioritized Requirements)

- Pipeline: sentence split, then Section labeling, then cutting sentences
  into clause-sized Chunks with Candidate phrases, then the Validation
  Round (Jev picks the phrase that names each Requirement), then the
  Refinement Round (drops Filler, merges duplicates, finds Alternative
  Groups, scores Importance).
- Each Requirement carries a Tier (`required | preferred | mentioned`),
  derived in code from the strongest Section it appears in. Tier accuracy:
  96.0% required, 73.8% preferred, 88.7% mentioned.
- Last measured: recall 93.6%, precision 87.5%, F1 90.4% on the
  20-posting golden set (rescore of the last clean run after the label
  review).

## Resume Checker (Requirements + Resume → evidence and Coverage)

- Resume parsing: a markdown Resume becomes Evidence Units, one per
  bullet, prose sentence, skills line, or education/certification entry,
  each with its Resume Section, role, company and dates.
- Retrieval Round: per Evidence Unit, a Jev Choice over all Requirements
  plus `none`, repeated over the best 16.
- Strength Round: per retrieved pair, a yes/no gate decides whether to
  link at all, and a separate Choice grades strong, partial or weak. That
  Choice has structured options and "not evidence" options (competing
  tool, shared words only, different skill, context only). Skills and
  Summary lines are capped at weak, so they get only the gate; other
  pairs are graded only after their gate passes.
- Fit Score (ADR 0002): a 0–100 Tier-weighted average of Coverage, plus a
  score per Tier and the Gaps (required items with no evidence).
- Coverage: each Requirement gets its best link's strength or `none`; an
  Alternative Group gets its best member's.
- CLI: `cmd/check` writes the coverage JSON, with a debug trace that logs
  every pair, its probabilities and the accept/reject decision, plus run
  metrics.
- Docs: new terms in `CONTEXT.md`; ADR 0001 for the two-round design.

## Eval

- 11 pairs of a golden posting and a Resume: 6 synthetic Resumes plus the
  user's 2 masked Resumes, each against 2 postings.
- `make eval-checker` runs it live; `eval -checker -rescore <report.json>`
  re-scores stored runs offline after label edits. A test checks the
  labels for broken quotes and invalid strengths.
- Fit error: the mean gap, in points, between the Fit Score from the
  predicted Coverage and the one from the labeled Coverage (lower is
  better).
- `eval -checker -baseline` also scores every pair with Reactive Resume's
  one-prompt match score through the generative client, and compares the
  two on Fit error, time and cost per pair. Jev cost is now split by
  stage (retrieval vs strength) in every report.

## Tuning so far

Every config ran 3+ times; identical runs differ by about 0.5 points.

| Step | Coverage |
|---|---|
| First design | 81.3% |
| + "not evidence" options in the grading Choice | 84.7% |
| + structured `{what, not_for, examples}` options | 84.8% |
| + yes/no gate per pair + multi-round retrieval | 86.9% |
| Cheaper config: skip unused grading questions, one narrowing round (current default) | **87.4%** |

- Required Requirements: 83.9% → 88.0% across the last two rounds.
- Fit error: 3.2 points (was 4.0).
- Tried and rejected: keeping more candidates per retrieval round, peeling
  off one winner per round, Score-based grading, and a "needed capability"
  negative option. Yes/no-per-Requirement retrieval reached the same
  Coverage but costs more and is less precise.
- The TypeSafe docs review (one judgment per question, structured option
  boundaries, a Choice to rank and a yes/no to decide) drove the last
  round of gains.

## Cost and the generative baseline

- Jev bills input tokens only (about $0.042 per million), so cost comes
  down to prompt length, not the number of calls.
- Half of the grading questions were never used (Skills/Summary lines are
  capped anyway; gate-rejected pairs never use their grade), and a third
  retrieval round changed nothing. Skipping both cut cost 23% ($0.0053 →
  $0.0041 per pair) at the same speed (7.4s per pair) and the same or
  better accuracy (15 runs over 5 configs).
- Against Reactive Resume's one-prompt match score (`claude-opus-5`, 3
  runs):

| | Jev | Generative |
|---|---|---|
| Fit error, mean (worst) | 4.0 (13)* | 6.4 (26) |
| Time per pair | 7.4s | 16.6s |
| Cost per pair | $0.0041 | $0.029 |

  \* Measured with the previous default; the current default scores 3.2.
  Adding the Requirement Extractor (about $0.0056 per posting) gives about
  $0.010 when each posting is checked against one Resume, still about 3×
  cheaper.
- Caveat: both are scored against our own labeled Coverage, which favors
  the Jev view of fit. The generative model judges more broadly (domain,
  seniority). Real outcomes (interview vs rejection) are still needed as a
  neutral target. Generative input tokens are estimated, because the
  local proxy misreports them.

## Committed

On `chore/project-setup`, not pushed: every commit since `main`
(extractor tuning, Tier, Resume Checker, its eval, Fit Score, the Skills
line split, the competing-tool guards, the generative baseline, and the
cost options with the new default). Each builds and passes tests on its
own. The side branches `exp/generative-baseline` and `exp/cheaper-jev`
are merged and can be deleted.

## Next steps

1. **Consider career-ops scoring signals.** career-ops
   (`~/code/career-ops`, `modes/_shared.md`, `modes/oferta.md`) scores an
   offer 1–5 with one LLM pass: a weighted average of CV match, North Star
   alignment, comp, cultural signals and red flags (4.5+ apply now, 4.0+
   worth applying, below 3.5 skip), plus a separate posting-legitimacy
   tier. For each signal, decide whether to add it and how to get it
   cheaply:

| career-ops signal | What it measures | Cheapest way here |
|---|---|---|
| Match con CV | Each JD requirement mapped to CV lines; gaps marked hard blocker vs nice-to-have, adjacent experience | Already done: Coverage, Tier, Gaps, Fit Score |
| Archetype + North Star alignment | Role type (6 archetypes, or a hybrid of 2) vs the user's target archetypes | One Jev Choice per Job Description over the archetypes; compare with a target list in a profile file (code) |
| Role summary (Block A) | Domain, function (build / consult / manage / deploy), seniority, remote (full / hybrid / onsite), team size | One Jev request per Job Description, one Choice each; team size from text in code |
| Level (Block C) | JD level vs the candidate's natural level | Jev Choice on the JD plus one per Resume role (once per Resume); Resume dates give years in code |
| Comp (Block D) | Salary vs market | JD salary extracted in code or by Jev; market data needs an outside source (out of scope for Jev) |
| Cultural signals | Culture, growth, stability, remote policy | Jev Choices on the company / benefits sentences the extractor drops today |
| Red flags | Blockers and warnings, lowering the score | Knockout constraints (location, work authorization, clearance, language) as Jev Choices on JD sentences, checked against a profile; a failed one caps the score |
| Posting legitimacy (Block G, separate) | Posting age, apply button active, tech specificity, requirements realism, reposts, layoffs, salary transparency | Mostly metadata from the scraper (Hermes); tech specificity and realism as Jev Choices on the JD |

   Output: a short design (ADR) listing the signals to adopt, how each is
   computed, and how they combine with the Fit Score (separate scores, a
   cap, or one weighted score), then measure each against real
   application outcomes before keeping it.

2. **Write the "Scoring savings" document.** How Jev saves money on
   resume scoring compared with generative models, for readers outside
   this repo:
   - why it is cheaper: a classifier billed on input tokens only, no
     generated text, small questions instead of the whole Resume and Job
     Description in one prompt;
   - the measured numbers: Jev $0.0041 vs $0.029 per pair (about 7×),
     $0.010 vs $0.029 with the extractor, 7.4s vs 16.6s, Fit error 3.2 vs
     6.4;
   - where the cost goes (retrieval vs strength) and what cut it 23%
     (skip unused questions; one narrowing round);
   - how it scales with volume: one Resume against many postings, and
     what can be computed once per Resume or per posting;
   - the caveats: scored against our own labels, estimated generative
     input tokens, 11 pairs, no outcome data yet.

## Open items

1. User review: the masked Resumes (committed but not yet checked by the
   user; the commit can be amended before push) and the labels, especially
   partial. The labels are a first draft, and some were added from the
   model's own output, which flatters precision.
2. Remaining errors are almost all on partial: about 15 per run are graded
   strong, and about 14 are dropped by the gate.
3. Overfitting: thresholds and options were picked on the same 11 pairs,
   and offline gains have started shrinking live. 4–6 fresh pairs are
   needed to confirm the numbers.
4. Outcome check: score real applications (Reactive Resume holds their
   status) and test whether the Fit Score ranks interviews above
   rejections, against Reactive Resume's own match score.
5. More cost cuts, not yet tested: a shorter grading rubric (flat v3 is
   27% smaller than v5) and shorter retrieval option descriptions.
6. Possible new signals beyond requirements, cheapest first: recency and
   years from Resume dates (code only), knockout constraints such as
   location or work authorization, and seniority match (overlaps with
   the career-ops review in Next steps).
7. Deferred: Importance ranking, the tailoring judge and writer loop, and
   caching.
