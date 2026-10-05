---
status: proposed
---

# Match Score blends the Fit Score with a Holistic Round

The replacement target is the generative match score (product review v1).
Against it, the Fit Score alone (ADR 0002) ranks pairs poorly (Kendall
τ-b ~0.45 on the 30-pair development subset) and runs ~17 points low.
Knob calibration could fix the level only by hurting the ranking
(`docs/tuning.md`). The misses are holistic: a posting for high-school
students scored 53, and many-detail postings are averaged down.

We add one Jev request per pair, the **Holistic Round**, over the full
posting and the whole Resume (it needs no extracted Requirements, so it
runs alongside extraction):

- `core_work` (Score, 5 levels): how much of the job's core work the
  candidate has done themselves, as a 0–1 share.
- `blocker` (Noul): P(a stated hard eligibility condition is clearly
  unmet by the Resume); an unmentioned condition is not a blocker.

Product-domain and career-level questions were tried and dropped: they did
not track the reference.

## Definition

x = (0.5 × Fit/100 + 0.5 × core_work) × (1 − blocker)

Match Score = clamp(round(21.2 + 107.9 × x), 0, 100); null when the Fit
Score is null. The Fit Score stays as defined in ADR 0002 and is still
reported, with its traceable Coverage.

The weight and the linear map were fitted on the development subset of
one run (2026-10-05T02-59-15Z): Kendall τ-b 0.67, leave-one-out MAE 8.8
(Fit Score with the same kind of map: 12.4). Accept after repeated runs
confirm it; refit only on development data; the final held-out set is
never used for fitting.
