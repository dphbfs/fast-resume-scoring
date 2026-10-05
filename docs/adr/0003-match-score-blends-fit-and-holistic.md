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
runs alongside extraction). The Match Score uses three of its answers:

- `responsibilities` (Score, 5 levels): how much of the job's day-to-day
  responsibilities the candidate has carried out, in any domain (0–1).
- `domain_mismatch` (Noul): P(the job's product or industry domain is one
  the candidate has never worked in).
- `blocker` (Noul): P(a stated hard eligibility condition other than
  location is clearly unmet by the Resume); an unmentioned condition is
  not a blocker. (Including location made it misread a US-or-Canada
  posting.)

It also records `primary_gap`, `location_mismatch`, and the employment
gap (from Resume dates, in code); they add nothing on the development
subset (one resume), and the final set decides whether they stay. Tried
and dropped: core work, domain closeness, career level, role type,
transferable scope, soft eligibility (`docs/tuning.md`). Why these
signals: `docs/gap-analysis-2026-10-05.md`.

## Definition

Match Score = clamp(round((44.2 + 31.7 × Fit/100 + 46.2 × responsibilities
− 17.5 × domain_mismatch) × (1 − blocker)), 0, 100); null when the Fit
Score is null. The Fit Score stays as defined in ADR 0002 and is still
reported, with its traceable Coverage.

The weights are a least-squares fit on the 30-pair development subset in
scoring mode, pooled over three runs (2026-10-05T12-24-46Z, 12-25-55Z,
12-27-01Z): per run MAE 5.5-5.6, bias ~0, τ-b 0.79-0.81; with pairs held
out, MAE 6.4, τ-b 0.78. Confirmation run 12-30-35Z: MAE 5.6, 80% within
±10, τ-b 0.80. Refit only on development data; the final held-out set is
never used for fitting.

History: v2 used the blocker with location and weights 42.8 / 48.4 /
38.9 / −19.4 (MAE 5.8-6.3). The first version blended the Fit Score with `core_work`
(`(0.5 × Fit/100 + 0.5 × core_work) × (1 − blocker)`, mapped linearly):
leave-one-out MAE 8.8 full mode, 9.3 scoring mode, τ-b 0.64–0.67.

## Scoring mode

The Match Score does not use Importance or mentioned-tier Requirements, so
the scoring path runs the pipeline lean: `PIPELINE_SKIP_IMPORTANCE`,
`PIPELINE_SKIP_RESPONSIBILITIES`, `CHECKER_SKIP_MENTIONED`, and one
Retrieval round (`CHECKER_NARROW_SIZES=none`). On the development subset
this cut cold Jev cost from ~$0.015 to ~$0.0092 per pair (Requirements per
posting 52 -> 31) for ~0.5 points of MAE (leave-one-out 9.3 vs 8.8). The Fit
Score in scoring mode covers required and preferred Requirements only.
