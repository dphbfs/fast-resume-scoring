# Final report: replacing the generative match score with Jev (review v1)

Date: 2026-10-06. Plan: `docs/review-v1-plan.md` (step F2). Decisions:
ADR 0004 (Match Score v4). Experiment log: `docs/tuning.md`.

## Verdict

Match Score v4 replaces the generative match score (Claude Opus 5, one
prompt per resume and job). On data it was never tuned on, it agrees with
the generative score as closely as an independent frontier-model recruiter
judge does, ranks pairs the same way, and costs about 300× less per score
at about 18× lower latency. Accepted on 2026-10-06.

| | Jev Match Score v4 | Opus 5 (generative) |
|---|---|---|
| Error vs generative score, sealed set | MAE 8.6, τ-b 0.78 | judge reference: MAE 8.8, τ-b 0.78 |
| Cost per score | $0.0001 | ≈ $0.033 (lower bound) |
| Latency per score | 0.3 s in process, 0.8 s CLI cold | 14.1 s (p95 20.4 s) |
| Run-to-run spread, same input | ±1–2 points | ±1 point (single-call MAE vs median 1.1) |

## What the system does now

One Jev request per (resume, job) pair, the **Holistic Round**, over the
full posting and the whole resume, asks three typed questions:

- `role_match` (Score, 5 levels): how closely the kind of role the
  candidate has been doing matches the job's;
- `experience_short` (Noul): clearly under-qualified (years of the
  relevant kind of experience, or level) for this job;
- `blocker` (Noul): a stated status condition (student, clearance,
  license, citizenship) clearly unmet.

`Match = (80.7 + 18.5·role_match − 55.2·experience_short) × (1 − blocker)`,
clamped to 0–100. Entry point: `bin/score -jd job.txt -resume resume.md`
(JSON with the score and the three signals).

Requirement extraction and the Resume Checker are no longer on the
scoring path. They still work and can explain a score (Requirement
Coverage and Gaps); `eval -e2e -fit` runs them. Earlier designs that
built the score from Requirement Coverage (Fit Score, Match v1–v3) and
why they lost are in `docs/tuning.md` and ADR 0002–0003.

## Accuracy

The generative score of record is the reference tool's saved score: one
Opus 5 call per pair on an eval-only copy, never rescored. 95% intervals
are pair bootstraps (2,000 resamples).

| Set | Pairs, resumes | MAE vs reference | τ-b | Within ±10 | Max error |
|---|---|---|---|---|---|
| Development: Main resume subset | 30, 1 | 5.0 [3.3, 6.8] | 0.83 [0.66, 0.94] | 87% | 20 |
| Development: former first sealed set | 20, 4 | 5.0 [3.5, 6.7] | 0.88 [0.74, 0.97] | 90% | 13 |
| **Second sealed set (held out)** | 20, 4 | **8.6 [6.0, 11.1]** | **0.78 [0.63, 0.92]** | 60% | 22 |

On the sealed set, the recruiter judge (gpt-6-luna, a different model
family, 3 runs, median, quoted evidence required) scores MAE 8.8, τ-b
0.78 against the same reference. Jev vs judge: MAE 7.9. Development
numbers are optimistic: the question set was chosen among ~15 candidates
on those 50 pairs.

Errors by resume on the sealed set: new-grad backend 4.2, staff
platform/SRE 5.4, application security 9.8, a real career changer
(software engineer moving into data analysis) 14.8.

Known weak spots, accepted:

- **Career changers** are under-scored on roles in their new field
  (`role_match` ~0.55 for analyst roles); the judge often agrees with Jev
  there, so part of the gap is the generative score being generous.
- **Off-role or location-restricted postings** can be over-scored by
  10–15 points (a financial-reporting analyst role for an SRE: 38 vs 24).
  Location is deliberately excluded from `blocker`: including it caused a
  misfire on a US-or-Canada posting.

### How stable is the generative score itself?

- Same input, repeated calls: single-call MAE 1.1 vs the median of three.
- Same input, days apart: today's calls run +11.6 above the saved scores
  on average (range +4 to +24), so tuning used rescored values.
- Same pairs, resume as Markdown instead of the tool's JSON: Opus 5 scores
  14.9 points lower (every resume, −10 to −20). Jev, which reads the
  Markdown, sits closer to that replay (MAE 11.2) than the replay sits
  to the reference.

So "the same accuracy as the frontier model" is bounded by how much the
frontier model agrees with itself across days and input formats; an
8.6-point error is within that envelope.

## Cost and speed

Measured on the 20 sealed-set pairs (`scripts/opus_replay.py`,
`eval/replay/2026-10-06T11-43-15Z`). The proxy does not report input
tokens, so Opus input is estimated from characters (≈2,300 tokens with a
Markdown resume; the tool's JSON resume is larger, so this is a lower
bound). Output: 843 tokens mean, measured. Prices: Opus 5 $5 / $25 per M
tokens; Jev billed on input tokens only.

| | Jev v4 | Opus 5 | Ratio |
|---|---|---|---|
| Cost per score | $0.0001 | ≥ $0.033 | ≥ 300× cheaper |
| Latency, mean | 0.3 s (eval), 0.8 s (CLI, new process) | 14.1 s | ~18× faster |
| Latency, p95 | < 1 s | 20.4 s | |
| 20 pairs, 4 in parallel | ~2 s | 76 s | |

The project's target was ≥ 5× cheaper than one Opus 5 call. Match v3
(extraction + checking + Holistic Round) reached 5.4× at $0.0092 per
cold pair; v4 removes extraction and checking from scoring.

## Usage scenarios (measured with `bin/score`)

| Scenario | Result |
|---|---|
| Fresh single score (new process each) | 0.78 s mean, 1.6 s max |
| Repeat of the same pair (no cache) | 0.45 s; scores 57–60 across 5 calls |
| One resume × many jobs (18 postings, 8 in parallel) | 1.6 s wall |
| Many resumes × one job (9 resumes, 8 in parallel) | 1.2 s wall; data-oriented resumes rank top for a BI role (80, 73) |

No result cache is needed at this cost; Jev answers are near-deterministic
(question-level run-to-run difference ≤ 0.013), so a cache keyed by the
input hash would be safe if one is wanted. Concurrency goes through the
bounded Jev limiter (`JEV_MAX_CONCURRENCY`).

## Evaluation method

- Reference data: the reference tool's saved scores, imported and masked
  (`testdata/reference`, `testdata/final`, `testdata/final2`); eval-only
  copies scored once, then archived.
- Two sealed sets with pre-registered job picks (by title, before any
  scoring), resumes written by a separate agent from a fixed prompt plus
  masked real resumes. The first sealed set exposed Match v3 (MAE 11.6,
  τ-b 0.69) and became development data; the second validated v4.
- Selection for v4 used resume-held-out cross-validation (leave one
  resume out), since generalizing to new resumes was what failed.
- Secondary judge: `.claude/skills/recruiter-judge` via
  `scripts/judge.py`; `scripts/compare_judge.py` reports the three-way
  agreement.
- Jev spend for the whole review: about $5 of the $7.5 cap (summed from
  per-run costs; check the OpenRouter dashboard for the exact figure).

## Limitations

- Small samples: 70 scored pairs in total, 9 resumes; intervals are wide.
- One generative call per pair as the reference; that score itself drifts
  (see above).
- Opus cost is a lower bound (input tokens estimated, Markdown resume).
- The three question wordings are fitted constants; changing a word means
  re-probing (`scripts/probe_holistic.py`) and refitting
  (`scripts/fit_match.py`) on development data, then a fresh sealed set.
- Not yet wired into the job agent or the reference tool.

## Recommendations

1. Use `bin/score` (or `app.HolisticJudge` + `domain.MatchScore`) as the
   scoring path; keep extraction and the Resume Checker for "why this
   score" explanations, or freeze them.
2. Phase 2 hardening before production: config validation, input limits
   and a run deadline, sanitized provider errors (plan H2–H5).
3. If career-changer accuracy matters, treat it as a new tuning round:
   collect such pairs as development data and validate on a third sealed
   set.
4. Re-check agreement periodically against fresh generative scores; the
   generative scale itself moved by ~12 points within two weeks.
