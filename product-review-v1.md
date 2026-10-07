# Product Review v1

**The project is a promising local replacement for the generative model approach’s scoring, but it has not yet demonstrated equivalent scoring behavior.** The implementation already provides useful concurrency, explainable evidence, and encouraging API-cost savings. The next milestone should be proving agreement with the generative model approach on unseen resume–job pairs.

This report incorporates both clarifications: the generative model approach is the product benchmark, and deployment is a local CLI on personal computers or homelabs. Resume tailoring is excluded.

I reviewed the plan, implementation, tests, and recorded evaluations. `go test -race ./...` passed during the review. No code was changed and no paid model evaluations were run.

**Response to the alternative proposals:** The responses below preserve the original review and comments. Each states a recommended disposition and, where measurement is necessary, a concrete experiment and adoption condition. These are recommendations for settling the design, not claims that proposed experiments have already passed. This follow-up changes only this document; the test result above is from the original review.

## 1. Define success around the actual replacement goal

The target should be:

> Given the same resume and job description, produce a score sufficiently close to the generative model approach’s score, with lower end-to-end latency and API cost.

The generative model approach is a reasonable reference because you find its scores useful. Agreement with it establishes replacement quality; it does not independently establish hiring-outcome accuracy. Hiring outcomes are unnecessary for the immediate milestone.

The current evaluation answers a different question: how closely each system approximates your coverage-based formula. Jev is designed around that formula, while the generative baseline receives a general scoring prompt. Consequently, the reported lower Fit Score error does **not** establish closer reproduction of the generative model approach.

See [baseline.go](internal/adapter/eval/baseline.go#L78) and [checker_score.go](internal/adapter/eval/checker_score.go#L159).

Recommended evaluation:

| Measure | Purpose |
|---|---|
| Mean absolute difference from the generative model approach | Typical score disagreement |
| Percentage within ±5 and ±10 points | Understandable agreement bands |
| Largest disagreements | Detect consequential failures |
| Ranking agreement across jobs for one resume | Preserve the user’s prioritization |
| Agreement around any user decision threshold | Preserve practical decisions |
| End-to-end latency and API cost | Verify the replacement benefit |

The ±5/±10 bands are reporting suggestions, not established acceptance thresholds. Choose acceptance criteria after measuring the generative model approach’s own variability.

Use the exact production prompt, resume representation, model, and settings. The current baseline substitutes plain text for the JSON resume, which may affect scores. Repeat reference calls on a subset to establish how much the reference disagrees with itself.

> **Alternative proposal:** Agree that the current Fit error is circular (labels are scored with our own formula). But I would not make agreement with the generative model approach the *primary* target. Our baseline runs (`docs/tuning.md`, "Generative baseline") show the generative prompt missing the labeled fit by 6.4 points mean and 22–26 max. Matching it closely would mean copying its noise. The stated goal is frontier-level *accuracy*, so:
>
> 1. **Primary reference:** a blind holistic judgment per held-out pair, written by the user before seeing either system's output (a 0–100 score plus apply/skip).
> 2. **Score both arms against it the same way:** MAE, share within ±10, Kendall τ across jobs for one resume, and apply/skip agreement. Ranking and the decision are what the user actually consumes.
> 3. **Secondary column:** agreement with the generative model approach, plus its self-variance (3 calls per pair). This tells us how close a "replacement" can get at all.
> 4. **Add a strong frontier arm** as the accuracy ceiling: a rubric prompt that receives the requirements list, temperature 0, median of 3. Beating the generative model approach's minimal prompt is a weak claim on its own. The comparison should be against the best a frontier model can do at a reasonable price.
>
> Agree on passing the JSON resume to the generative arm.

**Response — retain the generative model approach as the primary replacement benchmark; adopt blind human review as a secondary check.** The user's clarification explicitly selects the generative model approach as the useful product reference. The 6.4-point difference from our coverage formula is not evidence that the generative model approach is noisy or wrong: that formula is precisely the disputed reference. Only repeated reference calls measure its self-variance. Matching its stable behavior and copying random fluctuation are different objectives.

Use the median of three reference calls as a more stable evaluation target, retain the individual scores to report variability, and report the operational latency/cost of one production reference call separately from the cost of collecting three benchmark samples. Record the actual model/version and request settings; a proxy alias alone is insufficient provenance.

Collect blind human judgments where practical, but anchor the scoring rubric and define apply/skip as **resume fit only**. Otherwise salary, location preferences, and personal interest introduce criteria this product does not claim to score. Those labels are valuable for adjudicating consequential disagreements and may later justify changing the target; one person's unanchored 0–100 score is not automatically an objective accuracy standard. Keep human labels used for calibration separate from held-out labels.

A stronger frontier prompt is a useful optional challenger, not an accuracy ceiling and not a prerequisite for this replacement milestone. Its end-to-end arm must receive the raw documents and produce or pay for its own requirements. Giving it golden requirements would create a different, diagnostic comparison. Freeze its prompt before testing; use deterministic settings where supported without assuming temperature zero eliminates variance. Report Kendall τ-b within each resume's job set, with enough jobs per resume to make ranking meaningful.

**Settlement:** primary = agreement with the actual generative scorer; secondary = blinded fit judgments, decision/ranking agreement, and reference variability. Add the stronger frontier arm after the minimum benchmark works, or alongside it if collecting it is inexpensive. Do not change the primary target on the basis of the existing circular comparison.

## 2. The implementation has a sound foundation

Several architectural decisions are worth retaining:

- **Traceable scoring:** requirements → evidence links → coverage → deterministic score.
- **Independent evidence processing:** each unit progresses from retrieval to strength assessment without waiting for all other units.
- **Bounded provider concurrency:** separate classifier and generative-client limits.
- **Cancellation and retries:** contexts propagate into HTTP requests; retry backoff includes jitter.
- **Useful evaluation tooling:** saved predictions, offline rescoring, confusion matrices, and cost tracking.
- **Evidence-based optimization:** removing an unnecessary retrieval round and skipping unused grading reduced recorded cost.

This decomposition fits Jev’s documented design: atomic questions evaluated independently, with results combined in application code. [TypeSafe documentation](https://docs.typesafe.ai/introduction)

I would preserve this structure rather than rewrite the application.

## 3. Accuracy validation is the largest unfinished task

The checker evaluation uses golden requirements, so it excludes extraction errors. The same 11 pairs have also been used repeatedly to select prompts and thresholds; those pairs reuse eight resumes, including only two real resumes. Label review remains unfinished.

These are useful development fixtures, but insufficient evidence of generalization.

Recommended changes:

- Keep the existing fixtures as a development suite.
- Create a held-out set with different resumes and jobs; avoid splitting pairs so that the same resume appears on both sides.
- Evaluate the complete extraction-and-checking path against the generative model approach.
- Include realistic mismatches: career changes, adjacent technologies, seniority differences, incomplete resumes, and jobs with many alternatives.
- Keep requirement-level labels for diagnosing disagreements, rather than using their derived score as the primary replacement benchmark.

Repeated runs measure stochastic variability. They do not compensate for repeatedly tuning on the same examples.

The current evaluator also cannot simply be reused unchanged for end-to-end evaluation: `ScoreCheck` iterates predicted requirements. Missing extracted requirements need explicit alignment against a fixed reference set to avoid disappearing from diagnostic metrics. [checker_score.go](internal/adapter/eval/checker_score.go#L159)

> **Alternative proposal:** For alignment, reuse the extractor eval's existing loose matcher (`internal/adapter/eval/score.go`) to map predicted Requirements onto golden ones. An unmatched golden Requirement counts as Coverage `none`, and an unmatched prediction counts as an extra. No new matching logic is needed. Size and discipline of the held-out set: Fit MAE is about 3 and run noise about 0.5, so we need roughly 20+ held-out pairs to separate the arms. Freeze the config (commit hash plus config fingerprint) before the first held-out run, run it once per milestone, and never tune on it.

**Response — reuse the matching machinery, but do not treat every loose match as semantic equivalence.** The existing matcher accepts token overlap and shortened phrases; these can erase a duration, negation, scope, or proficiency qualifier. Use strict/alias matches first, expose loose matches for review, and store reviewed ambiguous mappings as fixture data. No new model-based matcher is needed. Preserve one-to-one matching and report duplicates separately.

For a golden requirement absent from extraction, record both `extraction_missing` and effective predicted coverage `none` in the diagnostic table. Report extraction misses independently even when the resume's labeled coverage is also none. Unmatched predictions remain extras. For golden-aligned diagnostic scores, use golden tiers/groups and separately report predicted tier/group errors; for the primary replacement metric, use the application's actual final score, including its actual requirements, tiers, groups, and extras. Never replace the production score with a golden-corrected score when claiming end-to-end agreement.

Twenty or more pairs is a reasonable initial smoke benchmark, not a justified sample-size calculation. Mean error and repeated-call noise alone do not determine how many independent examples are needed. Include multiple jobs for each of several held-out resumes; report paired differences with uncertainty clustered by resume rather than pretending all pairs are independent. Expand the set if the comparison is inconclusive.

**Settlement:** adopt strict-first matching with reviewed loose matches and a frozen milestone test. Record code/config/prompt/model versions and input hashes. Use development data for all fitting and selection. Once test examples or their error patterns guide changes, retire them into development and collect a fresh final test set; repeated milestone inspection otherwise becomes indirect tuning.

## 4. Coverage alone may not reproduce the generative model approach’s judgment

The Fit Score uses fixed coverage credits and tier weights. Those values are design assumptions, not calibrated mappings to the generative model approach.

Potentially missing signals include:

- Duration and recency of relevant experience.
- Whether a technology is central to someone’s work or mentioned incidentally.
- Seniority and scope of responsibility.
- Domain experience.
- Evidence spread across several statements or jobs.

Do not add all of these speculatively. First group the largest score disagreements by cause. Add a signal only when it explains a repeatable difference.

The code-side score formula remains useful: it is cheap, inspectable, and calibratable. Start with a small number of interpretable parameters rather than a complicated scoring model.

> **Alternative proposal:** Before adding any signal, calibrate the knobs we already have. There are five: the strong/partial/weak credits and the preferred/mentioned tier weights relative to required. Fit them by least squares against the blind holistic scores from §1. It is a few lines of code and the result can be rescored offline. Add a new signal only if the residual stays large *and* clusters by cause. Duration/recency is the most likely candidate, and §6 covers it without calling Jev.

**Response — adopt constrained calibration before adding signals, with a smaller search and the agreed target.** Fit against development-set generative reference scores; use blind human scores as a separately reported check, not a silently substituted objective. Never fit against the held-out labels proposed in §1.

Keep none = 0 and strong = 1 initially, so complete direct coverage still means full coverage. Fix required weight as the scale anchor. Tune partial/weak credits and preferred/mentioned relative weights subject to `0 <= weak <= partial <= 1` and `0 < mentioned <= preferred <= required`. That leaves four adjustable values, not five. If a persistent ceiling mismatch later justifies changing strong credit, treat that as an explicit score-semantics decision.

Jointly tuning tier weights is not ordinary linear least squares: the weights also change each pair's denominator. A small bounded grid or constrained optimizer is sufficient. Prefer a coarse, stable solution near current values to a narrowly optimal fit on a small sample. Fit unrounded scores and evaluate the actual rounded output. Start with weight calibration alone, then test alternative probability scoring separately so attribution remains clear.

**Settlement:** adopt constrained offline calibration on development data, with MAE and ranking reported alongside the fitting loss. Promote only if improvement survives grouped validation without materially worsening large disagreements. Add new signals only for persistent, interpretable residuals. Duration arithmetic can be code-only; establishing what the dates actually prove is not solved by arithmetic alone.

## 5. Partial evidence is the clearest current classification weakness

A recorded run matching current defaults reports 87.4% overall coverage accuracy, but the class breakdown is uneven:

| Labeled coverage | Correct predictions |
|---|---:|
| Strong | 110/116 — 94.8% |
| Partial | 19/48 — **39.6%** |
| Weak | 12/13 — 92.3% |
| None | 109/109 — 100% |

Seventeen partial cases become strong; eleven become none. These errors can cancel in the aggregate score while still producing misleading explanations.

Source: [recorded evaluation](eval/reports/checker/2026-10-02T02-58-00Z.md#L29).

Review partial labels before further prompt tuning. Distinguish direct instances, prerequisites, adjacent technologies, and minor involvement during analysis; the current category covers several different relationships.

> **Alternative proposal:** Score from the probabilities, not the argmax. Use credit = Σ P(class) × credit(class) over strong/partial/weak, normalized by evidence mass. The explanation keeps showing the argmax label. Benefits:
>
> - It removes the cliff at the strong/partial boundary, where most of the 17 partial→strong errors sit.
> - It should also reduce the ~0.5-point run-to-run variance.
> - It costs nothing to test: the trace already stores the probabilities, so it is an offline `-rescore` experiment.
>
> In the same rescore, also measure Fit sensitivity with partial merged into strong and into weak. If the Fit error barely moves, then partial-label accuracy is an *explanation* problem, not a *score* problem, and it should be prioritized below §1 and §3.

**Response — adopt this as a development experiment, not an immediate scoring replacement.** Let `mass = p_strong + p_partial + p_weak`. The proposed `sum(p_class * credit_class) / mass` is credit **conditional on the pair being evidence**. Normalization removes the grader's non-evidence mass: a pair with 0.05 strong and 0.95 alternative_tool receives conditional credit 1. This smooths the strong/partial boundary but does not resolve gate/grader disagreement. Reject malformed distributions and handle zero mass explicitly.

Specify the whole score before testing: preserve current retrieval/link eligibility and Skills/Summary caps, take the maximum pair credit per requirement, and then the maximum member credit per alternative group. Do not sum repeated bullets or assume independent evidence. Label the numeric contribution separately from the displayed argmax strength; a score no longer derived solely from Coverage needs an explicit formula version and trace fields.

Offline replay requires the per-pair traces, not just saved coverage results. These traces are gitignored and must be present. Gate-first runs omit grades for rejected pairs, and capped units may have no grade at all. They support this experiment only with the old eligibility/cap policy preserved; changing eligibility requires complete older traces or new calls. The existing `-rescore` path does not implement these new formulas automatically.

**Settlement:** compare the current discrete score, conditional expected credit, and the two partial-credit extremes on the development set. Report per-pair/ranking/decision changes and variance across repeated runs, not just mean error; cancellation can hide important failures. Promote smoothing only if it improves the agreed score metrics without unacceptable regressions. The claim that it reduces variance remains a hypothesis.

## 6. Concrete correctness and robustness issues

| Issue | Consequence | Suggested action |
|---|---|---|
| Gate passes while grader strongly favors a negative option | A pair can still receive a strong link | Record disagreement and evaluate targeted reassessment or an uncertain outcome |
| Years requirements are excluded from evaluation but scored in production | Reported quality does not cover actual behavior | Support temporal assessment or explicitly report unsupported requirements |
| Dates are parsed but omitted from classifier state | Duration cannot be assessed reliably | Combine relevant evidence with employment intervals |
| Section recognition depends on a specific Markdown convention | Formatting changes caps and interpretation | Validate the convention or normalize common formats |
| Input and response invariants are incompletely validated | Silent mis-scoring or crashes on malformed data | Validate IDs, groups, tiers, answer types, and probability values |
| Numeric configuration accepts non-finite/out-of-range values | Invalid settings can bypass logical checks | Require finite probabilities and valid bounds |

The gate/grader issue is particularly important. `decideStrength` chooses only among strong/partial/weak; a dominant `alternative_tool` probability does not prevent linking when the gate passes and veto is disabled. Your experiments show that a simple veto also rejects legitimate evidence, so another blanket threshold is not a demonstrated fix. [checker.go](internal/app/checker.go#L459)

For years requirements, dates are absent from `stateOf`, and independent statements cannot establish cumulative experience. Account for overlapping employment intervals; do not assign every skill the duration of an entire job. [checker.go](internal/app/checker.go#L160)

The parser expects `# Section` and `## Role | Company | dates`. A common `# Name` / `## Skills` document does not establish the intended Skills section. Also, detailed accomplishments placed under Summary are always capped at weak. [resume.go](internal/app/resume.go#L52)

One concrete response-handling failure: peel retrieval accesses `ranked[0]` without checking for an empty probability map. [retrieval.go](internal/app/retrieval.go#L123)

> **Alternative proposal (gate/grader):** Replace the veto threshold with a soft combination. Use link mass = P(gate) × P(strong+partial+weak) from the grader, then threshold the product (or feed it straight into the §5 expected credit). With this, a dominant `alternative_tool` lowers the link's weight instead of flipping it. The trace stores both answers for graded pairs, so this can also be tested by offline rescore.
>
> **Alternative proposal (years):** Handle years in code, with no Jev call:
>
> 1. Parse N from the Requirement value (`(\d+)\+?\s*years`).
> 2. Take units linked strong or partial to the Requirement's skill Requirements that share its Context Sentence.
> 3. Merge their roles' date intervals so overlaps are not double-counted.
> 4. Compare the total with N.
>
> This matches the review's "don't assign every skill the whole job" caveat only partly: it credits a role in which the skill was evidenced. That is a defensible approximation and it is explainable.
>
> **Alternative proposal (parser):** The product's resumes live in a resume builder as structured JSON, with sections and items that carry position, company, and dates. Make a structured JSON → Evidence Unit adapter the primary input and keep the Markdown parser as a fallback. This removes the formatting-convention risk, provides dates for the years logic, and gives the generative baseline arm the same representation.
>
> **Alternative proposal (peel):** Delete `peel` (see §11) instead of fixing it.

**Response — gate/grader: test soft scoring, but reject the interpretation of the product as a calibrated link probability.** Gate and grader evaluate overlapping evidence and their errors may be correlated. Neither their marginal outputs nor their product establishes a joint probability of correctness. Thresholding the product also creates a new hard cutoff; it is not a wholly soft solution.

There is a useful simplification: multiplying the proposed conditional expected credit by `gate * mass` produces `gate * sum(p_class * credit_class)`. Compare that heuristic against the simpler unnormalized `sum(p_class * credit_class)` and the §5 conditional variant. This distinguishes whether the gate adds value or merely discounts the same uncertainty twice. Keep hard-link semantics fixed for the first experiment; do not adopt a new threshold and new credits simultaneously. For capped units, keep the existing weak-credit rule until a separate policy is evaluated. Complete graded traces are required to replay candidates rejected by gate-first.

**Settlement:** approve a small offline ablation; keep current production link logic until an alternative passes development validation and the frozen final comparison. Do not delete the veto on the assumption that soft combination has already solved its failure cases.

**Response — years: reject the proposed rule as proof of a duration; accept it as explicitly qualified supporting information.** Sharing a context sentence does not establish which skill the duration modifies. “Five years of backend engineering; experience with Go and Kubernetes” does not assert five years of either technology. A partial link to a prerequisite is even less suitable. A single migration bullet in a ten-year role also does not demonstrate ten years of that skill.

Represent the duration's subject explicitly and prefer direct/strong evidence. Parse minimums, ranges, months, missing dates, and ongoing roles deliberately; unsupported wording returns unknown. Merge intervals only after linking the correct subject, and pin an as-of date for reproducible evaluation. Report whole-role spans as `roles containing evidence span X years; skill duration unverified`, not “X years of skill confirmed.” Missing dates must not become zero years. Exact temporal credit requires explicit duration evidence or a separately validated policy; do not silently insert this heuristic into Fit Score. Do not double-count a skill and its duration unless that weighting is intentional.

**Response — parser: adopt a local structured JSON adapter as the preferred structured input.** Keep the scoring core independent of that vendor: both JSON and Markdown adapters should produce the same internal evidence units, with source item IDs and dates. Read an exported file; no account access or live integration is needed for this milestone. Verify the actual export schema/version and handle rich-text bullets, visibility, custom sections, and incomplete dates explicitly. Preserve source references and test equivalent JSON/Markdown inputs. The reference arm receives the actual production resume representation; both arms must see equivalent substantive content. Structured JSON fixes metadata ambiguity, not whether a Summary achievement deserves a weak cap.

**Response — peel: adopt deletion if done in the same cleanup milestone.** Remove its flags, branches, and mode-specific tests together and return a clear error for removed configuration values. If deletion is deferred, add the empty-ranking guard now. Retained modes still need general response validation.

## 7. Concurrency is already used in the most valuable place

The checker processes independent evidence units concurrently and pipelines their dependent operations correctly. Keep that design.

Use concurrency where it overlaps meaningful independent work:

| Operation | Recommendation |
|---|---|
| Different evidence units | Keep concurrent processing |
| Section labeling and generated job summary | Run concurrently; join before validation |
| Independent jobs in a batch | Use bounded concurrency and one shared provider limiter |
| Dependent retrieval rounds | Keep sequential |
| Gate-first grading | Keep dependent unless choosing the combined-request mode |
| Parsing, sorting, score aggregation | Keep sequential unless profiling proves otherwise |

Summary generation currently runs after section labeling even though successful generation needs only the job description. Its fallback needs section results, but that does not prevent starting the generative request earlier. [extractor.go](internal/app/extractor.go#L75)

Do not interpret “concurrency everywhere possible” as “one goroutine for every small operation.” Extra scheduling and synchronization would add maintenance cost without materially reducing remote-model latency.

For large batches, bound active unit work as well as HTTP requests. Currently, all unit goroutines can exist and marshal requests before obtaining a provider slot.

> **Alternative proposal:** I agree with overlapping the summary and section labeling. I disagree with bounding unit goroutines. At CLI scale (under ~200 units), the goroutines plus a few KB of marshaled body each are negligible, and a second semaphore adds code without any measured gain. In batch mode, bound at the *job* level instead (`errgroup.SetLimit` on jobs, with one shared Jev limiter).
>
> Where concurrency actually buys latency:
>
> 1. **`JEV_MAX_CONCURRENCY=8` is the real bottleneck.** A pair makes about 72 calls, and each unit makes about 4 dependent requests. Real resumes, which have more units, take 11–12 s versus 4–5 s for synthetic ones, so time scales with unit count. That points to queueing. Measure queue wait (§9), then raise the limit up to the provider's rate limit. This is a one-line config change and likely the biggest latency win available.
> 2. **Batch mode:** pipeline each job, so checking job N starts as soon as its extraction finishes instead of waiting for all extractions.
> 3. **A combined `score` command:** parse the resume while extraction runs. This is trivial.
> 4. **Optional:** let later stages (strength) outrank first-round retrieval in the limiter to shorten the tail. Only do this if the queue-wait metric shows idle slots at the end of a run.

**Response — adopt job-level bounds for this CLI; withdraw a second unit semaphore as a default requirement.** With enforced input limits and bounded jobs, the unit count is already bounded. Keep one shared provider limiter and measure memory before adding another scheduling layer. The “few KB” body estimate and ~200-unit limit should be verified/enforced rather than assumed, since every initial retrieval carries the requirement options.

Concurrency 8 is a plausible queueing bottleneck, not yet a demonstrated one. Longer resumes also create more input tokens and dependent work. Add queue-wait, HTTP latency, in-flight counts, and retry/429 measurements, then compare limits 8, 16, and 32 within the provider's permitted limits, with repeated representative runs. Choose the lowest setting near the latency plateau that does not increase failures or throttling. A concurrency limit is not a requests-per-minute or tokens-per-minute limit.

Adopt a bounded per-job pipeline: extract a job, then immediately check it while other jobs continue extracting. Parse a shared resume once; it can overlap with extraction if the combined command can express that cleanly, but parsing is unlikely to provide a measurable latency win.

**Settlement:** add instrumentation, sweep concurrency, and pipeline jobs. Defer stage-priority scheduling. Idle slots at the tail mean insufficient ready work or serial/provider delay; reprioritizing an empty queue cannot fix that. Consider priority only if traces show critical-path requests waiting behind substantial queued independent work and a controlled experiment demonstrates a benefit without starvation.

## 8. Prioritize reducing remote work over micro-optimizing Go

A representative current-default run makes **788 calls across 11 pairs**, approximately 72 calls per pair. Remote calls and queueing are the first places to investigate. [Recorded run](eval/reports/checker/2026-10-02T02-58-00Z.md#L23)

Recommended optimization order:

1. **Reuse repeated work.** Cache extracted requirements and completed comparisons locally, using content hashes plus model, prompt, parser, and scoring versions.
2. **Overlap summary generation with section labeling.**
3. **Measure gate-first versus combined gate-and-grade requests.** Gate-first saves grading tokens but adds a dependency and resends state.
4. **Experiment with cheaper retrieval.** Lexical/alias matching or embeddings could propose candidates for Jev verification. Treat this as an experiment requiring recall measurements.
5. **Investigate targeted reassessment.** Spend extra work only on ambiguous or score-sensitive cases.

For one resume against many distinct jobs, caching does not eliminate first-time extraction for each job. Its largest savings come from repeated jobs, reruns, or multiple resumes against the same posting.

The existing summary cache needs stronger semantics before broader reuse: it omits model identity, does not deduplicate concurrent misses, and writes directly to the final file. Add versioned keys, `singleflight`, and atomic replacement. [cache.go](internal/adapter/gencache/cache.go#L43)

> **Alternative proposal:**
>
> - **Item 3 is already measured.** `docs/tuning.md` ("Cheaper Jev") shows gate-first saved 4% of the cost and added about 1.3 s per pair. Cost is already about $0.004 per pair (Jev bills input only, about $0.042 per M tokens, roughly 7× cheaper than the generative baseline). Latency is now the scarcer resource for a CLI, so consider going back to the combined gate+grade request: about +$0.0002 per pair for about −1.3 s. Re-run 3× to confirm.
> - **Before item 4 (lexical/embedding prefilter), try a code-only cut.** Retrieval is about 55% of input tokens because every unit's request carries all N Requirement options, each with its Context Sentence. Drop the Context Sentence from the second (16-option, already narrowed) round, or keep it only for short or ambiguous values (≤ 2 words). That is cheaper to build than embeddings and needs no new dependency.
> - **Add an item between 1 and 2: drop Importance questions in scoring mode** (see §11). This directly reduces extraction cost.
> - **Cache key:** one fingerprint hash over every `CHECKER_*` / `PIPELINE_*` setting plus prompt and criteria versions, reusing the labels-fingerprint idea, rather than hand-picking key parts.

**Response — accept the simpler experiment order, with corrections to the claimed savings.** The recorded gate-first comparison is A versus B under narrow `[16,8]`: it saved about $0.0002 and added about 1.3 seconds. Current defaults use narrow `[16]`. The direction is credible, but transferring the exact difference to the current configuration is an inference.

Make the next live latency experiment an alternating, repeated comparison of combined versus gate-first requests with narrow `[16]`, capped grading still skipped, and all other settings fixed. Prefer combined requests for the interactive default if they consistently improve end-to-end latency while preserving reference agreement and a material cost advantage. Keep gate-first only if its savings matter for the measured batch workload. Do not change the default solely from the old non-identical comparison.

Test second-round context reduction before adding embeddings. Compare context retained versus omitted after narrowing; shorter request size does not guarantee unchanged recall or lower latency. A two-word cutoff is only a heuristic: long requirements can also depend on qualifiers or alternative relationships in context. Promote only with preserved required-evidence recall and final-score agreement. Keep the first-round context intact for this ablation.

Adopt skipping Importance in scoring mode, with one implementation caveat: refinement currently uses `r.importance` for its kept metric, and result construction/order also needs auditing. Remove the question and its required answer coherently; represent “not computed” explicitly rather than pretending zero is a model judgment. Preserve the existing extraction mode's contract.

**Settlement — cache:** use canonical serialized **resolved configuration**, not raw environment strings, plus input hashes, provider/model identity, prompt/criteria content versions, parser version, and score version. Do not include API keys. One full semantic fingerprint is a safe first implementation; irrelevant settings may cause extra cache misses but must not cause incorrect reuse. Missing variables and explicit defaults must produce the same key. Mutable model aliases need an expiry/refresh policy; do not treat `latest` as immutable. Record broader execution settings separately for benchmark reproducibility.

## 9. Measure local-user performance fairly

Your recorded experiments are encouraging: approximately $0.0041 per checker pair and 7.4 seconds mean latency under the documented setup. Those are historical measurements, not guaranteed CLI performance.

The checker price excludes requirement extraction. The documented extraction estimate brings classifier spending to roughly $0.010 for a fresh one-job/one-resume comparison, before any additional generative-summary cost.

Report four scenarios separately:

- Fresh job and resume, single comparison.
- Repeated comparison using cached artifacts.
- One resume against many new jobs.
- Multiple resumes against one job.

Include p50/p95 elapsed time, total API cost, retries, and failures. Measure provider queue wait separately from HTTP time.

The current timing comparison uses different boundaries: checker duration includes internal queueing, while baseline duration excludes its limiter wait. Normalize those boundaries before claiming a speed multiplier.

Add an overall run deadline. HTTP timeouts alone do not bound semaphore waiting and retry backoff.

> **Alternative proposal:** Agree. Keep it minimal: one `context.WithTimeout` at the CLI root, set by a `RUN_DEADLINE` env (for example, 120 s by default). Contexts already propagate everywhere, so no other change is needed.

**Response — adopt for a single comparison, with explicit batch semantics.** A 120-second default is a reasonable initial guardrail, not a measured latency target. Validate the value, document any explicit opt-out, and keep signal cancellation as the parent context. Report timeout as an error, not a zero fit score.

For a future batch command, do not apply the same 120-second deadline to an arbitrarily large invocation. Give each started job a deadline and allow an optional separate overall batch budget. Queued jobs remain subject to cancellation of the batch. Context propagation already covers network waits and limiter acquisition; synchronous parsing/serialization/file I/O is not automatically interrupted by a context, which is another reason to keep input limits.

**Settlement:** add the root timeout for the current single-comparison CLI and test cancellation while queued, during HTTP work, and during retry backoff. No extra timeout abstraction is needed now.

## 10. Security priorities for a personal CLI

A multi-tenant service architecture is unnecessary. Distributed queues, fairness between users, and service authentication should not become prerequisites.

The relevant risks are more practical:

- **Accidental resource or spending amplification:** bound input bytes, evidence units, requirements, and optionally total request budget.
- **Private resume text in files and logs:** outputs contain evidence; traces contain additional text; provider error bodies can enter logs. Use restrictive permissions and bounded/redacted errors.
- **Remote processing expectations:** make it clear that a locally installed CLI still sends resume content to configured providers.
- **Document-based score manipulation:** test instruction text, negation, copied job requirements, and claims about other people. Typed answers do not establish resistance to adversarial input.

No successful prompt-injection exploit was demonstrated in this review. It remains a testable risk; OWASP specifically describes resume-based recommendation manipulation. [OWASP guidance](https://genai.owasp.org/llmrisk/llm01-prompt-injection/)

> **Alternative proposal:** These are concrete findings to add:
>
> - **Error bodies:** `jev.APIError` embeds the full response body (read up to 10 MiB) in `Error()`, which then goes to slog. Truncate it to about 512 bytes. ([client.go](internal/adapter/jev/client.go#L68))
> - **File permissions:** CLI output and traces use `os.Create` (0666 & umask), and eval reports, traces, and the cache use 0644/0755. Anything that holds resume text should be 0600/0700. ([cli.go](internal/adapter/cli/cli.go#L129), [checker_runner.go](internal/adapter/eval/checker_runner.go#L371))
> - **Injection tests:** make them cheap and concrete. Add 3–4 adversarial fixtures to the development suite: hidden instruction text, the job's requirements pasted into a Skills line, and "managed a team that used Kubernetes". Note in the report that capping Skills and Summary at weak is already a designed defense against keyword stuffing, and that typed answers mean injection can only shift probabilities. It cannot change the output format.

**Response — adopt the concrete fixes, but narrow the security claims.** Limit and sanitize provider error text before it enters `APIError`/logs, and do the same in the generative client. About 512 bytes is a sensible display budget; truncate safely and retain HTTP status and a provider request ID when available. Truncation alone is not redaction: the first 512 bytes can still contain resume text or credentials. Prefer allowlisted error fields, and keep the existing response-body size bound as a separate protection.

Use 0600 files and 0700 directories for private resume artifacts. Merely changing the mode passed to `OpenFile`, `WriteFile`, or `MkdirAll` does not tighten already existing files/directories. Ensure permissions on app-owned private locations, and use restrictive temporary files plus atomic replacement for app-managed cache/output files where appropriate. Do not chmod arbitrary user directories. Permissions also do not prevent accidental git commits: keep private runs outside the committed, explicitly masked evaluation corpus.

Adopt the adversarial development fixtures, with expected behavior defined before seeing outputs. Distinguish legitimate self-reported skills from text instructing the model to alter its judgment. “Managed a team that used Kubernetes” is ambiguous evidence about management versus hands-on operation, so include explicit positive/negative controls rather than declaring all such statements false. Add negation and copied job-text variants.

The weak cap limits the reward for keyword stuffing but still grants credit, and relies on correct section parsing. Typed interfaces constrain the intended output shape; they do not guarantee evidence truth or replace adapter validation. Do not claim that arbitrary provider responses are structurally impossible. Unit tests can validate parsing/caps; actual resistance to model manipulation requires live model fixtures, not mocks alone.

**Settlement:** implement bounded/redacted errors and private artifact handling now; add a small adversarial set to the existing development evaluation without presenting it as comprehensive protection.

## 11. Simplify the supported application

- Keep experimental rubrics and retrieval modes in named experiment presets; expose a smaller supported configuration.
- Centralize defaults and validation. Directly constructed checkers currently receive different fallback behavior from environment-configured ones.
- For scoring-only operation, test removing Importance questions because Importance currently does not affect Fit Score.
- Preserve the existing interfaces and explainable domain model; they already support testing and provider replacement.
- Defer compensation, cultural preferences, and posting-legitimacy features. They expand the product without proving the requested replacement.

> **Alternative proposal:** Delete rather than preset. The experiment log in `docs/tuning.md` plus git history already preserve the losing variants. Candidates to delete:
>
> - retrieval modes `peel`, `noul`, `single`
> - `CHECKER_STRENGTH_MODE=score`
> - criteria v1–v4 and v6
> - `CHECKER_VETO_THRESHOLD`, if §6's soft combination replaces it
> - `CHECKER_NARROW_STOP_P`
>
> Every one of them doubles the config matrix that tests and validation must cover, and deleting them also removes the `peel` bug. Centralize defaults in one `DefaultCheckerConfig()` that both the env loader and direct construction use.

**Response — favor deletion of rejected production variants, staged around the remaining experiments.** Git history and the tuning log are sufficient to preserve unsuccessful implementations if each report records the exact commit, settings, inputs, and available traces. Removing a mode does not require deleting historical reports.

Remove `peel`, `noul`, Score grading, criteria v1–v4/v6, and early narrowing-stop support once no queued experiment depends on them. Keep only narrow retrieval and v5 as the supported baseline. `single` is potentially useful for one final retrieval-cost ablation; keep it only in an experiment path if that experiment is actually scheduled, otherwise delete it. Remove the disabled/rejected veto as cleanup independently of whether the soft-combination experiment succeeds; retain disagreement trace data. Keep the gate-first switch only until the §8 default decision is measured, unless both interactive and batch policies earn their maintenance cost.

“Every option doubles the matrix” is not literally true for enums or dependent options, but the maintenance concern is correct. Remove their env parsing, validation, constructors' fallbacks, docs, and dedicated tests together; reject obsolete supplied settings rather than silently ignoring them. Historical offline rescoring should retain original config metadata without needing to execute deleted modes.

Adopt `DefaultCheckerConfig()` plus one validation path. Direct callers should start from defaults and override deliberately; constructors validate their input rather than guessing whether a zero value means “disabled” or “missing.” Tests needing explicit zero settings must still be able to express them.

**Settlement:** delete losing runtime implementations, centralize defaults, and preserve reproducible history. Do not accumulate a permanent experiment framework merely to retain old branches.

**Recommended next milestone:** build a held-out, end-to-end comparison against the actual generative production scorer; fix boundary validation and unsupported temporal scoring; then optimize the largest measured latency and cost contributors. That sequence gives you evidence that each optimization preserves the behavior the end user already trusts.

> **Alternative proposal (sequence):**
>
> 1. Run the offline rescore experiments first, since they cost $0: expected credit (§5), soft gate×grader (§6), and calibrated knobs (§4).
> 2. Build the held-out set with blind holistic labels, scoring the generative model approach, a strong frontier arm, and Jev end-to-end (§1, §3).
> 3. Fix boundary validation, error-body truncation, and file permissions, and delete the experimental modes (§6, §10, §11).
> 4. Address latency: measure queue wait, raise Jev concurrency, overlap the summary, and revisit gate-first (§7, §8).
>
> Steps 1 and 3 are cheap and independent, so they can run in parallel with labeling for step 2.

**Response — adopt parallel preparation, but establish targets/data before calibration and keep the final holdout unopened.** Zero additional API cost is not zero implementation effort. Calibration cannot run meaningfully before development reference scores exist, and replay cannot invent omitted grades. Human labels collected for a held-out set must never become the fitting target for the offline experiments.

Recommended settled sequence:

1. **Lock the evaluation contract:** actual generative scorer inputs/settings, primary agreement metrics, secondary human fit rubric, and acceptance tolerances chosen on development data before final testing. Inventory usable reports/traces and collect missing development reference scores. Reserve a separate final test set with several jobs per resume.
2. **Do independent low-risk work while preparing data:** validate boundaries, bound/redact error messages, protect private artifacts, centralize defaults, remove abandoned modes, and add the structured JSON adapter. These can be separate implementation tasks; this review does not itself authorize paid runs or modify runtime code.
3. **Run development-only offline ablations:** current discrete credit, conditional/unconditional expected credit, soft gate combination, and constrained parameter calibration. Report missing-trace/grade limitations and retain a simple baseline. Select one candidate before touching final held-out scores.
4. **Measure latency on development inputs:** queue time first, then a bounded concurrency sweep, summary overlap, per-job pipelining, combined versus gate-first, and second-round context reduction. Change one factor at a time and preserve the agreed accuracy checks. Skip Importance in scoring mode once its output/ordering dependencies are handled.
5. **Freeze and evaluate end-to-end:** compare the selected implementation with the actual generative scorer on the reserved set, including cold-run extraction/summary cost and wall time. Report uncertainty, ranking by resume, large disagreements, failures, and separate cached/batch measurements. Use the stronger frontier arm as an optional challenger, not a presumed truth source.

**Decision rule:** correctness/privacy fixes and the structured-input/defaults simplifications can proceed without waiting for model experiments. Numerical scoring and performance defaults change only on development evidence, then receive one frozen final evaluation. If that evaluation is inconclusive, expand independent test data; if it drives redesign, treat the inspected examples as development data and obtain a fresh final set. This settles what to build and how to decide, without claiming unmeasured improvements.
