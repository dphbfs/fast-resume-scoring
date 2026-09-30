# Golden set

Hand-reviewed Job Descriptions used by `make eval` to score the Requirement
Extractor against live Jev. Terms follow `CONTEXT.md`.

Per fixture:

- `<id>.txt`: the Job Description (job title on the first line).
- `<id>.json`: metadata (source, date, title, company, source URL).
- `<id>.expected.json`: the expected Requirements, drafted by Claude and
  corrected by hand.

Imported from Reactive Resume with `scripts/import_jds.py`. Two source formats:

- **Remote Rocketship** (and unknown source): the aggregator's own rewrite,
  with `Summary:` / `Role description:` / `Role requirements:` / `Benefits:` /
  `Tech stack:` blocks. The `Summary` is generated text and `Tech stack:`
  repeats skills from the body.
- **Greenhouse**: the original posting text, including company boilerplate.

## `expected.json` format

```json
{
  "requirements": [
    { "value": "Go", "aliases": ["Golang"], "tier": "required" }
  ],
  "alternative_groups": [["Go", "Ruby", "Python"]],
  "filler": ["self-starter", "eligible to work in the United States"],
  "notes": "free text for labeling decisions"
}
```

- `value`: the atomic Requirement as the extractor should name it.
- `aliases`: other surface forms that count as a match (case-insensitive).
- `tier`: expected Importance band, used to check ordering rather than exact
  scores:
  - `required`: stated as needed ("must have", "N+ years", "requirements").
  - `preferred`: bonus or nice-to-have.
  - `mentioned`: appears only in responsibilities or a tech stack list.
- `alternative_groups`: sets of Requirement `value`s the employer accepts
  interchangeably ("such as Go, Ruby, or Python"). Each member is also listed
  in `requirements`, with the tier of the sentence it came from.
- Responsibilities ("Own SDK development") are Requirements with tier
  `mentioned`.
- A years qualifier belongs to the Requirement ("5+ years distributed
  systems"); the bare phrase is an alias, not a second Requirement.
- `filler`: phrases the extractor should drop as Filler. Emitting one counts
  as a false positive. Includes non-skill conditions (work eligibility, visa,
  background check, on-call, location).

Scoring (implemented later in `make eval`): recall and precision over
`requirements` by value/alias match, Alternative Group agreement, plus the
tier ordering of Importance.
