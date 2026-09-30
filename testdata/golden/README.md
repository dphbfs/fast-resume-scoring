# Golden set

Hand-reviewed Job Descriptions used by `make eval` to score the Requirement
Extractor against live Jev. Terms follow `CONTEXT.md`.

Per fixture:

- `<id>.txt`: the Job Description (job title on the first line).
- `<id>.json`: metadata (source, date, title, company, source URL).
- `<id>.expected.json`: the expected Requirements, drafted by Claude and
  corrected by hand.

Imported from the generative scorer with `scripts/import_jds.py`. Two source formats:

- **Remote Rocketship** (and unknown source): the aggregator's own rewrite,
  with `Summary:` / `Role description:` / `Role requirements:` / `Benefits:` /
  `Tech stack:` blocks. The `Summary` block and the `Tech stack:` line are
  written by the aggregator, not the employer.
- **Greenhouse**: the original posting text, including company boilerplate.

## `expected.json` format

```json
{
  "requirements": [
    { "value": "Go", "aliases": ["Golang"], "tier": "required" }
  ],
  "alternative_groups": [["Go", "Ruby", "Python"]],
  "acceptable": ["repair drift across stack templates"],
  "filler": ["self-starter", "eligible to work in the United States"],
  "notes": "free text for labeling decisions"
}
```

| Field | Scored as |
|---|---|
| `requirements` | Must be found (recall). A prediction matching one is a hit. |
| `acceptable` | Neither: extracting it is fine, missing it is fine. Excluded from precision. |
| `filler` | Must not be extracted. Counted separately as "Filler extracted", and in precision. |
| a second match of an already-found Requirement | A duplicate: reported separately, excluded from precision (a Refinement merge miss). |
| anything else predicted | An extra: counts against precision. |

## Labeling rules

Apply these in order; they decide every case so labels stay consistent.

1. **Only employer text.** Label only what the employer wrote. Phrases that
   appear only in the aggregator's `Summary:` block or `Tech stack:` line go
   in `acceptable` (the extractor may pick them up; they are not asks). The
   job title line is never a Requirement.
2. **Requirement = specific and checkable on a resume.** Every one of these
   is a Requirement, with no "too minor" exceptions:
   - a named technology, language, framework, tool, platform, service,
     standard, protocol, or certification (`ECR`, `WebTrust`, `ML-KEM`,
     `Filebeat`);
   - a domain or problem area (`payments`, `PKI`, `observability`);
   - a practice or skill (`API design`, `code review`, `incident response`,
     `threat modeling`, `mentoring`);
   - a qualification (degree, certification, years qualifier);
   - a responsibility that names one of the above (`SDK development`,
     `on-call incident response`).
   Named items in "such as" / "e.g." / "like" lists count too; list them and
   put them in an Alternative Group.
3. **Soft skills are `acceptable`.** Communication, collaboration,
   problem-solving, leadership phrased as a trait. A resume cannot evidence
   them reliably, and the Resume Checker will not score them. (If the posting
   names a concrete practice, e.g. "writing technical design documents",
   rule 2 applies instead.)
4. **`acceptable` also covers defensible but non-essential phrases:**
   responsibility clauses longer than about five words that restate a duty
   ("repair drift across stack templates"), and names of the company's own
   systems, teams, or products ("billing engine", "FinHub", "Loki" at Grafana).
5. **`filler` is what must never come out:** personal traits and attitudes
   ("self-starter", "fast-paced environment"), job conditions (location, time
   zone, work authorization, clearance, travel, on-call rotation as a
   condition), company marketing, headings ("Bonus Points"), and the job title.
6. **Anything else is left unlabeled** and counts as an extra when
   predicted: generic single words ("infrastructure", "performance",
   "engineering"), fragments ("years experience"), and padded or cut-off
   phrases.
7. **Tier = the employer's section.**
   - `required`: requirements / qualifications / "must" / "you have".
   - `preferred`: bonus, nice to have, preferred, "a plus", ideally.
   - `mentioned`: responsibilities, role description, or context only.
   A section heading beats the wording of the item; if a posting has no
   headings, use the wording.
8. **One entry per distinct thing.**
   - Synonyms and word forms of the same thing are one entry with `aliases`
     ("alerting" / "alerts", "Go" / "Golang"). The scorer already matches
     simple word forms and slash/hyphen-joined words.
   - A category and its examples are separate entries; the examples form an
     Alternative Group ("backend programming" + [Go, Ruby, Python]).
   - A years qualifier belongs to its skill ("5+ years distributed systems");
     the bare skill is an alias, not a second entry.
   - A general and a specific item named separately are separate entries
     (`AWS` and `AWS ECS`).
9. **`value` and `aliases` are verbatim** from the posting where possible, so
   an exact match is achievable. A paraphrased `value` needs a verbatim alias.

## Scoring

`make eval` matches predictions to `requirements` one-to-one: strict when the
value or an alias equals the prediction after normalization; loose when the
prediction pads it with up to 3 words, shortens a multi-word label by one
word, or overlaps with Jaccard >= 0.6. Words are split at `/` and `-` and
compared by stem (alert/alerts/alerting). See `internal/adapter/eval`.

`eval -rescore <report.json>` re-scores a previous run's stored results
against the current labels without calling any API; use it after editing
labels.
