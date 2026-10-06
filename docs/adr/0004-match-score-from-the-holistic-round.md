---
status: accepted
---

# Match Score from the Holistic Round alone

Match Score v3 (ADR 0003) blended the Fit Score with Holistic answers and
was tuned on one resume. On the sealed final set (20 pairs, 4 new
resumes) it missed the generative reference by MAE 11.6, τ-b 0.69 (5.6 /
0.80 in development): wrong-role pairs floored near 40 (the intercept),
and the blocker read a missing degree and years as disqualifying. The
recruiter judge agreed with the reference (MAE 7.0, τ-b 0.88), so the
error was ours. Per F3 the final set became development data.

Refitting the v3 form on the 50-pair pool (5 resumes) did not help
(resume-held-out MAE 9.5). New Holistic questions did
(`docs/tuning.md`, "Redesign round 1"). The Holistic Round now asks three
questions, and the Match Score is

    (80.7 + 18.5 · role_match − 55.2 · experience_short) × (1 − blocker)

clamped to 0–100, where

- `role_match` (Score, 5 levels): how closely the kind of role the
  candidate has been doing matches the job's, from a different profession
  or specialty to the same kind of role and stack (0–1);
- `experience_short` (Noul): P(clearly fewer years of the relevant kind
  of experience, or a clearly lower level, than the job asks). In
  practice it reads as "under-qualified for this role";
- `blocker` (Noul): P(a stated status condition, such as current student,
  clearance, license, or citizenship, is clearly unmet). Years, degrees,
  skills, and location are qualifications, not blockers.

Evidence (development pool, 50 pairs, 5 resumes): resume-held-out MAE
5.2, τ-b 0.85; live runs MAE 5.0 on both the old subset and the former
final set. Adding the Fit Score, responsibilities, domain mismatch, or
must-haves does not lower held-out error. Holistic answers are
near-deterministic (run-to-run |diff| ≤ 0.013).

Consequences:

- Scoring is one Jev request: ~$0.0001 per pair and ~0.3 s (p95 < 1 s),
  about 500× cheaper than one Opus 5 call. `cmd/score` is the entry point
  (match schema v1: `match_score` plus the three signals).
- Extraction and the Resume Checker leave the scoring path. They remain
  for explaining a score (Requirement Coverage, Gaps) and run in eval
  only with `-fit`. Scoring-mode settings no longer affect the score.
- The three question wordings are fitted constants: changing a word means
  re-probing and refitting on development data.
- Selection was made among ~15 models on 50 pairs; the second sealed set
  (`docs/final-set-2/`) is the honest test.
