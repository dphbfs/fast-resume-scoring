# Reference scores

Development reference for the end-to-end eval (`docs/review-v1-plan.md`,
E1): saved match scores of the generative scorer (generative model approach,
Claude Opus 5) for the Main resume against real Job Descriptions.

Regenerate from a `list_applications` dump (includeArchived=true), with
`MAIN_RESUME_ID` set to the Main resume's ID in the job tracker:

```sh
scripts/import_reference.py <dump.json>
```

- `pairs.json`: one entry per pair: `id`, `title`, `company`, `resume`
  (the masked Main resume, `testdata/resumes/real-backend.md`), `jd`,
  `score` (0–100), the scorer's `strengths`/`gaps`, `golden` (the Job
  Description is also a golden extractor fixture), `subset` (fixed 30-pair, score-stratified tuning subset).
  `excluded` lists dropped applications with their reason; `counts`
  summarizes both.
- `jd/<id>.txt`: the Job Description as the scorer saw it (the whole
  field: header, condensed stack, full posting), with HTML entities
  decoded, emails/phones and contacts removed, and leaked job-agent
  bookkeeping above the `## <company> - <role>` title dropped.

Selection: Main resume only; Job Description >= 1,000 characters; a
single score (applications rescored to different values are excluded).

`noise.json` (E2, 2026-10-05): 10 score-stratified pairs (the shortest
Job Description per score band) were copied into eval-only applications
with the same Job Description text and the Main resume, scored 3× each,
and archived. It records each copy's scores, their median and range, and
the original saved score. Within-session noise was small (single call vs
median: 1.1 points mean, max range 8), but every copy scored above its
saved original (+11.6 mean, +4 to +24); see `docs/review-v1-plan.md`.

`current.json` (2026-10-05): the reference for tuning. Today's scorer on
38 pairs: the 30-pair tuning subset plus the 8 other noise pairs, each
copied into an eval-only application, scored (1 call; the noise pairs use
their 3-call median), and archived. Compared with the saved scores, 37 of
38 are higher (+11.9 mean, 0 to +26), but the order mostly holds
(Pearson 0.95, Kendall tau-a 0.80). The other ~57 pairs in `pairs.json`
keep only their saved scores, so use them for ranking checks, not for
absolute error.
