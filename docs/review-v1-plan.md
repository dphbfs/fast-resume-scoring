# Product Review v1: next steps

Source: `product-review-v1.md` (review, alternative proposals, settlements).
Scoring-only scope; resume tailoring is out. Terms follow `CONTEXT.md`.

## Decisions (Phase 0, settled 2026-10-04)

### Reference and data

- **Reference score:** the the generative scorer match scores already saved on
  applications (scored by Claude Opus 5). We pull applications, linked
  resumes, and scores into files. No copied scoring prompt.
- **New data:** eval-only resumes/applications may be created in the generative
  scorer, scored there, saved to files, then archived in the generative scorer
  to keep it clean. Real applications are never rescored (rescoring
  overwrites the saved score).
- **Selection:** only applications with a single score. A score that
  changed across job-agent notes and cannot be traced back is excluded;
  applications without a job-agent note count as scored once. Include recent
  applications (from 2026-09-29), archived ones too (mostly low scores,
  still useful). Exclude Job Descriptions under 1,000 characters (stubs).
- **Inventory (2026-10-04):** 1,232 applications, 118 with a saved score
  (113 on the Main resume, unchanged since 2026-09-24; 5 on two tailored
  copies). Base resumes: Main, Local (data/migration analyst), Contract
  SWE (no applications). No model/prompt metadata is stored with scores.
- **Storage:** pulled data committed under `testdata/` with neutral IDs.
  The user masks everything before the project is published; no
  scrubbing of the the generative scorer name is needed now.

### Noise, metrics, thresholds

- **Reference noise:** 10 eval copies of real applications, stratified
  across the score range (low, mid, high, near median), each scored 3×
  in the generative scorer (30 calls), saved, then archived.
- **Metrics:** MAE vs reference, share within ±5 / ±10, largest
  disagreements, Kendall τ-b across the Main resume's jobs. No
  apply/skip decision metric; the job agent is ignored in this phase.
- **Acceptance form:** relative to reference noise (start: Jev error ≤
  1.5 × a single reference call's error vs the 3-call median) plus an
  absolute cap (no pair off by more than 20 points). Final numbers are
  chosen on development data after the noise copies are scored.

### Cost and budget

- **Target:** at least 5× cheaper than one Opus 5 match-score call,
  measured **cold end-to-end** (extraction + checking for each new job).
  Estimate: Opus 5 ~$0.045–0.055 per call (resume JSON ~12k chars +
  JD + ~1k output tokens at $5 / $25 per M) → Jev must reach about
  **$0.010 per pair** (today ~$0.011). The Anthropic proxy does not log
  token counts, so measure Opus 5 cost with a cost-only replay: the
  the generative scorer JSON resume + JD through the proxy with the generative
  scorer's open-source prompt, recording tokens only (never used for
  scores).
- **Budget:** Jev ≤ $10, aim for $5. Start with the available
  OpenRouter balance (~$3.94; $10/month key cap) and top up only when
  it runs low. the generative scorer
  scoring is subscription-based; use it sparingly.
- **Jev limits:** TypeSafe allows 80 requests/s and 100K tokens/s for Jev
  1.13; OpenRouter adds no rate cap for paid models. No 429s recorded so
  far. Concurrency sweep may go to 32.

### Sets

- **Development set:** the Main resume's single-score pairs (~100 after
  filters), the Local resume, and the existing fixtures. Full runs only
  at checkpoints; tuning on a fixed 30-pair subset.
- **Final set (sealed until Phase 5):** Contract SWE + 3 synthetic
  resumes, 5 jobs each = 20 pairs. Jobs: unscored archived applications
  with JD ≥ 1,000 characters, not in the development or golden sets,
  picked by title before any scoring: 2 on-target, 2 adjacent, 1
  off-target per resume. Each pair becomes an eval application in
  the generative scorer, scored once, saved, archived.
- **Synthetic resumes:** written by another agent from a prompt we
  provide (lowers our bias), no access to fixtures or labels:
  (1) mid-level frontend/full-stack, (2) career changer (QA or
  support → backend), (3) senior data/ML engineer with gaps.

### Secondary judge

- No human blind scores (the user is not a recruiter).
- A local recruiter skill (no scripts, no tools): rubric from clawfu
  `resume-screener` and cowork-hiring-screener evidence rules ("no
  quote, no points", did vs near), its own holistic 0–100 score, strict
  JSON (`score`, quoted must-have evidence, `gaps`). Sees neither
  system's score.
- Model `gpt-6-luna` through the OpenAI proxy (`JUDGE_BASE_URL`,
  `JUDGE_API_KEY`, `JUDGE_MODEL` in `.env`), different family from the
  reference. 3 runs per pair, median.
- Scope: every final-set pair plus the 20 largest development
  disagreements.
- Proxy verified working with `gpt-6-luna` (2026-10-04).

### Models

- Claude usage is limited to Sonnet or Opus (no Fable).

## Phase 1: evaluation data and contract

- [x] E1. Pull script (`scripts/import_reference.py`, `testdata/reference/`): applications (incl. archived), linked resumes,
      saved scores, job-agent notes → apply the selection rules → files
      under `testdata/` with neutral IDs; record exclusions and reasons.
- [~] E2. Noise copies and current reference (`testdata/reference/noise.json`, `current.json`; today's scorer runs +11.9 above the saved scores, so tuning uses `current.json`; cost replay pending): pick 10 stratified applications, create eval
      copies in the generative scorer, score each 3×, save, archive. Cost-only
      replay of those pairs through the Anthropic proxy for Opus 5 tokens.
- [x] E3. End-to-end eval mode (`eval -e2e`, `make eval-e2e`; first run: MAE 20.5, bias -17.5, τ-b 0.46, $0.0168/pair, see `docs/tuning.md`; golden alignment deferred, only 2 reference pairs are golden): raw JD + resume → extract → check →
      production Fit Score compared to the reference score. Golden
      alignment for diagnostics only (strict/alias first, loose matches
      surfaced for review, `extraction_missing` + Coverage `none`,
      extras reported).
- [ ] E4. Metrics and report: MAE, ±5/±10 share, max error, largest
      disagreements, τ-b per resume, uncertainty clustered by resume, cold
      end-to-end Jev cost and wall time vs the Opus 5 cost.
- [ ] E5. Report provenance: commit, resolved-config fingerprint, prompt
      and criteria versions, model ids, input hashes.
- [x] E6. Final set: prompt for the synthetic-resume agent; job picks per
      resume; eval applications created, scored, saved, archived; sealed.
- [x] E7. Recruiter judge skill + runner: `.claude/skills/recruiter-judge`
      (rubric, sent as the system prompt) and `scripts/judge.py` (3 runs,
      median, verbatim-quote check; writes `<set>/judge.json`).
      `scripts/compare_judge.py` reports Jev vs reference vs judge.
      `eval -e2e -e2e-dir testdata/final -set current` runs the final set.

## Phase 2: hardening and simplification (no model calls, parallel with Phase 1)

- [x] H1. Delete losing variants (plus gate wording v1): retrieval `peel`, `noul`, `single`,
      Score grading, criteria v1–v4/v6, narrow-stop, veto. Reject obsolete
      settings with a clear error. Keep the gate-first switch until L5.
- [x] H2. `DefaultCheckerConfig()` + one validation path; finite
      probabilities, valid bounds. Done 2026-10-06: `config.DefaultConfig`,
      `config.Config.Validate`; the checker's divergent fallbacks removed.
- [x] H3. Boundary validation of inputs (IDs, refs, groups, tiers) and
      Jev answers (types, option names, finite probabilities, empty
      distributions). Jev answers done 2026-10-06 (`jev.AnswerError`);
      extract results read by `check` validated by `domain.Result.Validate`
      (all 150 cached real results pass).
- [x] H4. Input limits and a root `RUN_DEADLINE` (default 120 s); tests
      for cancel while queued, during HTTP, during retry backoff.
      Done 2026-10-06: 48 KiB per input file; deadline in extract, check,
      score (eval runs are unbounded).
- [x] H5. Sanitized provider errors in `jev` and `openai` clients
      (allowlisted fields, status, request id, ~512-byte cap).
      Done 2026-10-06: `platform/providererr`; key-like strings redacted.
- [x] H6. Private artifacts 0600/0700, atomic writes; summary cache with
      versioned key, `singleflight`, atomic replace.
      Done 2026-10-06 (`platform/fsutil`, Go outputs only; the Python eval
      scripts are unchanged). Cache key not versioned: it would invalidate
      the committed summaries, and extraction is off the scoring path.
- [ ] H7. the generative scorer JSON adapter: HTML bullets, free-text `period`
      dates with mixed dashes, hidden items, custom sections, degree text
      inside descriptions; source item ids; equivalence test with the
      Markdown adapter.
- [~] H8. Years-qualifier Requirements reported as qualified information,
      not in Fit Score. Obsolete for scoring: the Fit Score left the scoring
      path (ADR 0004); revisit only if the checker becomes the explanation
      feature.
- [x] H9. CLI notice that resume content goes to the configured providers.
      Done 2026-10-06: in the extract, check, and score usage text.

## Phase 3: development-only offline ablations

Needs E1 reference scores and full traces (pre-gate-first runs have all
grades).

- [ ] A1. Rescore support for named score-formula versions.
- [ ] A2. Variants vs current discrete credit: unnormalized Σp·credit,
      conditional Σp·credit/mass, gate·Σp·credit, partial = strong / =
      weak extremes.
- [x] A3 (first pass, `scripts/calibrate_fit.py`: MAE-best knobs hurt ranking, not adopted; see `docs/tuning.md`). Constrained calibration of partial, weak, preferred, mentioned
      against the development reference scores; grouped validation by
      resume.
- [ ] A4. Pick one candidate (or keep current); log in `docs/tuning.md`.

## Phase 3b: Holistic Round (added 2026-10-05)

- [x] H-R1. Holistic Round and the Match Score (ADR 0003, proposed).
- [x] H-R2. Gap analysis (`docs/gap-analysis-2026-10-05.md`) and new
      signals; Match Score = Fit + responsibilities − domain_mismatch,
      × (1 − blocker): subset MAE 5.8-6.3, τ-b 0.80-0.82 over 2 repeats
      with fixed weights, scoring mode ~$0.0092/pair. Confirm on the
      final set (also decides primary_gap, soft_eligibility, gap months).
- [x] H-R3. Gap-analysis round 2: blocker reworded without location
      (Grafana fix); role type, transferable scope dropped. Match Score v3:
      subset MAE 5.5-5.6, τ-b 0.79-0.81; pair-held-out 6.4 / 0.78.

## Phase 4: latency and cost (required to reach ~$0.010 per pair cold)

- [ ] L1. Instrumentation: queue wait, HTTP latency, in-flight count,
      retries/429.
- [ ] L2. Job Summary generation concurrent with section labeling.
- [x] L3. Skip Importance questions in scoring mode (`PIPELINE_SKIP_IMPORTANCE`; part of scoring mode, ADR 0003).
- [ ] L4. Concurrency sweep 8 / 16 / 32.
- [ ] L5. Combined vs gate-first requests, narrow `[16]`.
- [x] L6 rejected (lost 8% of links for ~$0.0008/pair). Replaced by scoring mode: required/preferred only, no responsibilities sentences, one Retrieval round; cold ~$0.0092/pair (about 5.4× cheaper than Opus 5). Was: second-round retrieval without Context Sentences (required for
      cost unless L3 alone reaches the target).
- [ ] L7. Batch command: job-level bound, shared Jev limiter, per-job
      pipeline and deadline.
- [ ] L8. Local cache for extraction results and comparisons.
- [ ] S1. Adversarial development fixtures (live Jev).

## Phase 5: freeze and final evaluation

- [x] F1. Freeze commit and config; run the sealed final set once: Jev
      end-to-end vs reference, plus the recruiter judge.
      Done 2026-10-06: Match MAE 11.6, τ-b 0.69 (dev 5.6 / 0.80); judge vs
      reference 7.0 / 0.88. See `docs/tuning.md`, "Final set, frozen run".
      Redesigned (ADR 0004, F3); second sealed set 2026-10-06: Match MAE
      8.6, τ-b 0.78, the same agreement as the judge (8.8 / 0.78). See
      `docs/tuning.md`, "Second sealed set, frozen run".
      Accepted 2026-10-06: v4 is the replacement score; final2 stays sealed.
- [x] F2. Report metrics with clustered uncertainty, τ-b, largest
      disagreements, failures, cost ratio vs Opus 5, and the four
      scenarios (fresh single, cached repeat, one resume × many jobs,
      many resumes × one job).
      Done 2026-10-06: `docs/final-report-v1.md`.
- [ ] F3. Inconclusive → more independent test data. Redesign → the
      final set becomes development data; collect a fresh one.
