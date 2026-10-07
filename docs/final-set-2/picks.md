# Second sealed set: job picks (pre-registered 2026-10-06, before any scoring)

The first final set became development data after the Match Score
redesign (plan F3, ADR 0004). This set tests Match Score v4.

Held-out resumes: three synthetic (`testdata/resumes/final2-syn-{1,2,3}.md`,
written by a separate agent from `synthetic-resume-prompt.md`) and the
user's real Data & Reporting Analyst resume (`final2-real-analyst.md`,
masked). The analyst resume's experience appears in two Resume Checker
fixtures (`real-data-analyst.md`), which v4 does not use; it was never
part of a Match Score fit.

Jobs were picked by title only, from job-tracker applications the
project had never read: the four created on 2026-10-05 (Praetorian,
Cloudflare for SaaS, Cockroach Labs, Samsara) plus older archived ones,
each with a cleaned posting ≥ 1,000 characters (three stubs were swapped
for the next title of the same kind). The pool has no mobile postings, so
the synthetic prompt's third candidate is a security engineer. Two
postings serve two resumes each (many resumes × one job), so pair IDs are
`<application id>--<resume stem>`. Jev was not run on any of them. List:
`testdata/final2/pairs.json` (`scripts/import_final.py <dump> final2`).

| Resume | On-target | Adjacent | Off-target |
|---|---|---|---|
| syn-1 new-grad backend (Go/Java) | Praetorian (SWE), Affirm (SWE II backend) | Breakthrough Talent (senior Go), Cloudflare for SaaS (senior) | CHEP (data analyst) |
| syn-2 staff platform / SRE | Staff Platform Engineer, Senior SRE | Staff DevProd (infra observability), Cockroach Labs (corporate security) | Frontline (reporting assurance analyst) |
| syn-3 application / cloud security | SeatGeek (SWE, security), Cockroach Labs (corporate security) | Identity & Auth infra (senior), RapidFort (senior Python/Go backend) | Verizon (business intelligence) |
| real data & reporting analyst | Travel + Leisure (compliance data analyst), Verizon (business intelligence) | Samsara (data engineer), Coinbase (staff data platform) | Search platform infra (senior) |

The set stays sealed until its frozen run: nothing is fitted or tuned on it.
