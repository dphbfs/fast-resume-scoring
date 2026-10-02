# Resume Checker labels

One file per (golden Job Description, Resume) pair:
`<resume>__<golden id>.expected.json`. Resumes live in `testdata/resumes/`
(`syn-*` are synthetic, `real-*` are the user's masked Resumes). Input
Requirements are the golden labels of the named Job Description. Terms
follow `CONTEXT.md`.

```json
{
  "job": "01a0d60b-07c3-7254-8218-ecb9908e4e46",
  "resume": "syn-platform",
  "skip": ["4+ years DevOps/SRE/Platform engineering"],
  "links": {
    "Kubernetes": [
      { "quote": "self-managed Kubernetes clusters with kubeadm", "strength": "strong" },
      { "quote": "Tools: Kubernetes, Helm", "strength": "weak" }
    ]
  },
  "notes": "free text"
}
```

- `links` keys are golden Requirement values. A Requirement that is not
  listed has Coverage none.
- `quote` is a substring of exactly one Evidence Unit (as `ParseResume`
  splits the Resume), so labels survive parser ID changes.
  Long Skills lines are split into chunks that repeat the label
  ("Technical Stack: ECS, Lambda, ..."): quote the chunk that holds the
  item.
- `skip`: Requirements left out of scoring (years qualifiers).
- Coverage = the best labeled strength.

## Strength rules

- **strong**: hands-on work with the Requirement or a direct instance of it
  (Kubernetes <- "ran services on EKS"; NoSQL <- "backed by DynamoDB").
  Degree and certification entries are strong for their Requirement.
- **partial**: a part, prerequisite, or the broader practice of it, or
  named with light involvement (Kubernetes <- "containerized services with
  Docker"; CodeIgniter <- "migrated a CodeIgniter storefront to Laravel").
- **not evidence** (leave unlisted):
  - a competing tool of the same kind (Kubernetes <- "Docker Swarm";
    BigQuery <- "Athena"; CakePHP <- "CodeIgniter");
  - shared words only (distributed systems <- "distributed system logs");
  - a different real skill (infrastructure-as-code <- "deployed with
    Docker");
  - only the surrounding role implies it (Kotlin <- "integrated BLE through
    the Android SDK").
- Broad Requirements ("backend services in production", "software
  engineering background") are labeled on every bullet that supports them,
  not only the best one.
- **weak**: named or implied only, with no demonstrated work. Skills and
  Summary units are always weak at most.

`go test` lints these files (`TestCheckerLabelsAreConsistent`).
