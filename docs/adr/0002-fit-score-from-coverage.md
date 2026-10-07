---
status: accepted
---

# Fit Score is a code-side formula over Coverage and Tier

The Resume Checker reports one Fit Score (0–100) per Job Description–Resume
pair, computed in code from Coverage and Tier only, with no extra Jev call.
We chose a formula over asking Jev for an overall judgment because Coverage
is already evaluated per Requirement, so the score inherits that accuracy and
every point can be traced to a Requirement; a single Jev Score would be a new,
unevaluated judgment over a 32k-token state with no explanation.

## Definition

Scored items: each Alternative Group counts once (its Coverage is its best
member's; its Tier is its strongest member's), and each Requirement outside a
group counts once.

| Coverage | Credit |   | Tier | Weight |
|---|---|---|---|---|
| strong | 1.0 | | required | 3 |
| partial | 0.6 | | preferred | 1.5 |
| weak | 0.3 | | mentioned | 1 |
| none | 0 | | | |

Fit Score = round(100 × Σ weight × credit / Σ weight). No scored items
gives no score (null), never 0 or 100.

Reported next to the score, never folded into it:

- the score per Tier (same formula restricted to one Tier), and
- the Gaps: required scored items with Coverage none, by ID.

Example: required strong, strong, partial, weak, none; preferred strong,
none; mentioned partial. Earned 3×2.9 + 1.5×1 + 1×0.6 = 10.8 of 19 → 57,
with one Gap.

## Considered options

- **Importance as weight.** Rejected for now: Importance has not yet been
  checked in a real-world test (CLAUDE.md), so weighting by it would add
  unverified noise. Revisit once that check is done, e.g. weight × (0.5 +
  0.5 × Importance).
- **Required Gaps as a knockout** (cap the score when any required item has
  none). Rejected: one missed retrieval would hide an otherwise strong
  Resume. Gaps are listed instead, so the reader decides.
- **Counting Evidence Links** (more evidence → higher score). Rejected:
  rewards repetition, and Coverage already takes the best link.

## Consequences

- Credits and weights are guesses until eval says otherwise. `cmd/eval
  -checker` should report Fit Score error: the score from predicted Coverage
  versus the score from labeled Coverage, per pair (mean and max absolute
  difference). This needs no new labels.
- Years-qualifier Requirements are scored like any other, but the Resume
  Checker has no years handling, so they tend toward partial or none. If eval
  shows they skew the score, exclude them from scored items.
- The score is only as good as the Requirements fed in: extractor misses and
  Filler pass straight through. Measure on extractor output, not only golden
  Requirements, before trusting it.
- Coverage schema gains an additive `fit` block (`score`, `by_tier`,
  `gaps`); still schema v1.

First measurement (2026-10-02, offline rescore of the 2026-10-01T02-16-53Z
run): Fit Score error mean 3.9, max 11 points over 11 pairs. The prediction
mostly runs low (labeled partial predicted none), and the largest errors
are on the pairs with the lowest Coverage accuracy.
