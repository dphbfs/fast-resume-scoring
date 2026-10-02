# Resume Checker tasks

Milestone: given golden Requirements and a plain-text Resume, produce an
evaluated many-to-many set of Evidence Links and per-Requirement Coverage.
Design: `CLAUDE.md` ("Resume Checker"), `docs/adr/0001`. Terms: `CONTEXT.md`.

- [x] 1. `tier` on extractor Requirements (code rule from Sections) + tier
      accuracy in the extractor eval report
- [x] 2. Resume parser: markdown convention -> Evidence Units with Resume
      Section, role, company, dates
- [~] 3. Fixtures: pull + mask the user's Resumes from the generative scorer, write
      synthetic Resumes, draft `testdata/checker/<pair>.expected.json`
      (drafted; user reviews masking and corrects labels)
- [x] 4. Retrieval Round (top K=5, p >= 0.02, `none` sink)
- [x] 5. Strength Round (mass >= `CHECKER_MIN_EVIDENCE_MASS`, Skills/Summary
      cap) + Coverage and Alternative Group Coverage
- [x] 6. `cmd/check` CLI, coverage schema v1 output, `-debug` trace, metrics
- [x] 7. `cmd/eval -checker`: retrieval recall, link P/R, strength accuracy,
      Coverage accuracy by Tier; first live run recorded in `docs/tuning.md`

Next (after the user reviews masking and labels):

- [ ] Fill label gaps on the real Resumes (many predicted "false links" are
      valid evidence), then `eval -checker -rescore` the baseline.
- [x] Negative Strength options (v3, default): Coverage 81.3 -> 84.7.
- [x] Multi-round retrieval (narrow, peel): retrieval recall +3-4, Coverage
      only up with a higher evidence mass; re-test after label review.
- [x] Noul retrieval: retrieval recall 97%, but Coverage only +0.8 with
      threshold 0.7 + mass 0.8 at 2x cost; not adopted (tuning log).
- [x] needed_capability negative (v4): no Coverage gain; selectable only.
- [x] TypeSafe guidance review; A (structured options), B (gate Noul),
      C (Score grading): A+B adopted with narrow retrieval, Coverage
      83.9 -> 86.9; C rejected (tuning log).
- [ ] Hold-out pairs: thresholds/options were picked on the 11 pairs;
      add 4-6 fresh pairs (new resumes or JDs) to confirm.
- [ ] Partial boundary: ~15 partial->strong, ~14 partial->none left;
      review partial labels first.
- [ ] Sharpen strong vs partial (v2 wording had no effect).
- [x] Fit Score (ADR 0002): `domain.ScoreFit`, `fit` block in the coverage
      output, Fit Score error in `eval -checker` (first rescore: mean 3.9,
      max 11 points).
- [ ] Fit Score on extractor output (end-to-end), not only golden
      Requirements.
- [ ] Check whether years-qualifier Requirements skew the Fit Score.
- [x] Split long Skills lines (> 6 items) so retrieval's K=8 cap doesn't
      hide listed skills.
- [ ] Gate vs grader disagreement: gate links while the grading Choice's
      top option is non-evidence (e.g. Golang CLI -> Node.js, strong).
