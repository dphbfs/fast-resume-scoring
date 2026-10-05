# Where the Match Score and the generative reference disagree (2026-10-05)

Question: why does the Match Score still miss the generative reference by
15–20 points on some postings? Method: the reference prompt (Reactive
Resume's match-score prompt, verbatim) plus a request to list every factor
that moved the score, each with a category, direction, weight 1–10, and
evidence (`scripts/explain_reference.py`). Model: Claude Opus 5 (the
reference model) through the local proxy, 3 runs per posting, Main resume
(masked Markdown). No eval was run. Raw replies:
`eval/explain/2026-10-05T11-55-26Z.json` (gitignored).

| Posting | Reference | Match (Jev) | Explained scores |
|---|---|---|---|
| Cyware, Senior SWE (Python/Django, FedRAMP) | 39 | 58 (+19) | 24, 27, 21 |
| Cloudflare Cloudforce One, Vulnerability Defense SWE | 76 | 57 (−19) | 55, 55, 55 |
| Airbnb, Senior SWE – Guest & Host (Java) | 84 | 84 (0) | 70, 73, 74 |
| Cloudflare, Senior SWE – PKI & Cryptographic Systems | 34 | 35 (+1) | 24, 23, 22 |

## Consistency

- Scores are stable: spread ≤ 6 points per posting across 3 runs.
- Factor categories are stable: almost every category appears in 3/3
  runs, and per-run category totals mostly vary by ±5. The exception is
  the largest positive, `role_responsibilities`, whose total swings more
  (Airbnb 39–47, Cloudforce One 24–38, Cyware 11–19).
- Asking for an explanation lowers the score 10–21 points against the
  reference on all four postings. Part of that is the explanation itself
  (the model enumerates more gaps), part is the input (next section). So
  the factor weights are relative signals, not a decomposition of the
  reference score.
- Cost note: each explained reply is ~4k output tokens and 35–50 s.

## Input fidelity problem (fix first)

The masked Markdown resume has no location; the resume Reactive Resume
sends has "Orlando, FL". Without it, the model inferred an EU/Ukraine base
("Ukrainian fluency", "EU leasing") and applied eligibility/location
penalties (−2 to −15) in all four postings, including a US citizenship
requirement it treats as likely unmet. The real reference saw a US
location, so these penalties are partly an artifact here. Jev's Holistic
Round reads the same masked resume.

**Action:** keep a city/state line in the masked resume (or mask it to a
plausible US city), and re-check the eligibility factors.

## Factors the generative scorer weighs that Jev does not

Totals are per run, summed over the factors in that category.

| Factor (category) | Where | Typical weight | Jev today |
|---|---|---|---|
| **Primary technology missing** (core_technology −) | Cyware: Python 7–10 yrs (−24 to −26); PKI: cryptography/PKI (−27 to −46); Airbnb: Kotlin (−6 to −10) | dominant negative | Fit Score weighs every required item equally, so one missing primary language is diluted among ~20 items; `core_work` captures it only partly |
| **Transferable responsibilities** (role_responsibilities +) | all 4: migrations, distributed systems, production ownership | +11 to +47, the largest positive | scoring mode drops responsibilities sentences and checks required/preferred items only; `core_work` is strict (Cloudforce One: 0.31) |
| **Domain / industry mismatch** (domain_or_industry −) | all 4, 3/3 runs: cybersecurity, identity, TLS/PKI vs EV charging | −4 to −23 | none (a 3-level domain-closeness Score was tried and dropped: τ-b 0.04) |
| **Eligibility / location** (eligibility_or_location −) | all 4: citizenship, hybrid city, state residency | −2 to −15 | `blocker` fires only on clearly unmet hard conditions (0.06–0.12 here); partly the input artifact above |
| **Employment gap / short tenure** (other −) | 3 of 4: last role ended Nov 2025; 6-month tenure | −2 to −9 | none (dates are parsed but unused) |
| Years / seniority, education (+) | all 4, near constant | +7 to +10 | absorbed by the Match map's intercept |

## The two large disagreements

- **Cyware (Jev +19 too high).** The reference is driven by the missing
  primary language (Python, 7–10 years required; −25 per run), missing
  secondary stack (Django, GovCloud: about −25), citizenship (−9 to −15),
  and the cybersecurity domain (−7 to −15). Jev's Fit Score (42) averages
  Python among many partly covered items, and `core_work` (0.34) and
  `blocker` (0.11) do not carry the primary-language or domain penalty.
- **Cloudflare Cloudforce One (Jev −19 too low).** The reference credits
  transferable distributed-systems and production-ownership work heavily
  (+24 to +38) and treats the security domain and location as moderate
  negatives. Jev's `core_work` (0.31) reads "core work" as security
  tradecraft the candidate has not done, and the Fit Score (41) misses the
  responsibilities match because scoring mode skips those sentences.

## What this suggests for Jev (not yet tested)

In order of expected impact:

1. **Fix the resume input** (location line), then re-check eligibility.
2. **Weight required items by centrality.** Either reuse Importance (now
   skipped in scoring mode) to weight the Fit Score, or ask one Noul per
   required item "is this the job's primary technology or core skill?",
   so a missing primary language costs more than a missing nice detail.
3. **Credit transferable responsibilities.** A Holistic Score such as
   "how much of the job's day-to-day responsibilities has the candidate
   carried out, regardless of domain?", next to the stricter `core_work`.
4. **Domain as a penalty, not closeness.** A Noul "is the job's product or
   industry domain one the candidate has never worked in?" (the dropped
   3-level closeness Score asked a different question).
5. **Soft eligibility.** A Noul for "a location, citizenship, or residency
   condition the resume makes unlikely to be met", separate from the hard
   `blocker`.
6. **Employment gap** from parsed dates, in code.

Each would need the usual check: offline replay where possible, then 3
live runs on the development subset, before any change to the Match Score.

## Round 2: the postings that still disagree (Match Score v2)

After adopting the Fit + responsibilities − domain-mismatch Match Score,
the largest remaining gaps (mean over runs 12-05, 12-10, 12-11) are
Grafana Databases (−22), Cyware (+17), Pantheon API Transformation (−16),
Cloudforce One (−13), Cloudflare Zero Trust (+12), Cloudflare Cache (−10).
The four not analyzed before were rerun through the explained prompt, 3
runs each, with the location fix in the resume (raw:
`eval/explain/2026-10-05T12-22-30Z.json`).

| Posting | Reference | Jev Match | Explained | Dominant generative factors |
|---|---|---|---|---|
| Grafana, Backend – Databases | 68 | ~46 (−22) | 56, 48, 52 | − database internals (−24 to −32); + observability tooling (+16 to +26: the candidate's Prometheus/Grafana work matches the company's product); + responsibilities |
| Pantheon, API Transformation | 76 | ~60 (−16) | 54, 52, 55 | − Go not the primary language (−39 to −49), − gRPC/GraphQL; + monolith decomposition (+20 to +30) |
| Cloudflare Zero Trust Client | 25 | ~37 (+12) | 15, 12, 16 | − no systems language (Rust/C/C++, −37 to −43); − no VPN/Windows-internals work (−14 to −18) |
| Cloudflare Cache | 66 | ~56 (−10) | 40, 47, 34 | − no Rust (−24 to −30); + distributed-systems design and ownership (+24 to +31); − CDN domain (−8 to −15) |

Consistency held again: every dominant category appears in 3/3 runs with
similar weights, and scores spread ≤ 13 points. The location fix worked:
eligibility is now positive for US-remote roles (+3 to +4) and a steady
−6 only for hybrid roles outside the candidate's city.

### Role positioning changes which factors dominate

Across all eight postings analyzed, two kinds of role behave differently:

- **Platform / distributed-systems roles** (Cache, Pantheon, Grafana,
  Cloudforce One): the reference forgives a missing primary language or
  domain because the work transfers. Transferable responsibilities are a
  large positive (+20 to +38). Jev under-scores all four (−10 to −22):
  its `responsibilities` reads them strictly (0.28–0.65).
- **Specialist roles** (Zero Trust kernel/VPN client, Cyware
  Python/FedRAMP, PKI cryptography): the missing primary technology or
  specialty dominates (−24 to −49) and transferable work earns little.
  Jev over-scores two of three (+12, +17).

So the same signal (`primary_gap`) means different things by role type:
it is ~0.9 for both Zero Trust (reference 25) and Cache (reference 66).
That is why `primary_gap` added nothing in the linear blend: its effect
depends on whether the role is specialist or general.

### Jev-side issues found

- **Blocker misread on Grafana:** `blocker` 0.43 and `soft_eligibility`
  0.75 for a posting open to the US or Canada ("Canada | Remote" is in the
  title); the reference rates location positive. With the blocker
  multiplying the score, this alone costs ~40%. All other pairs except the
  high-school fellowship (0.97) are at or below 0.26. Sharpening the
  discount (1 − blocker^k) without refitting adds +7 bias because every
  pair carries ~0.1, so the fix belongs in the question wording (state the
  candidate's location explicitly and ask about a listed location the
  candidate is outside of), not the formula.
- **`responsibilities` is too strict for platform roles** (Cache 0.28,
  Grafana 0.38) where the reference credits distributed-systems ownership.

### Candidate next experiments (not run)

1. A **role-type** question (Choice: specialist vs general backend/
   platform), and use `primary_gap` only for specialist roles (an
   interaction term). Expected to fix Zero Trust and Cyware without hurting
   Cache and Pantheon.
2. Reword `responsibilities` to credit transferable engineering work
   (designing, building, operating distributed services at the job's scale)
   separately from domain tasks; or add a second Score for
   "transferable engineering scope".
3. Reword `blocker`/`soft_eligibility` to compare a listed location with
   the candidate's stated location, and re-check Grafana.
4. A **product-tooling overlap** signal (the candidate already uses the
   company's product, e.g. Grafana/Prometheus at Grafana Labs) is a large
   positive for the reference; low priority, it is rare.

Each needs offline replay where possible, then 3 live runs on the
development subset; with interaction terms, watch overfitting on 30 pairs.
