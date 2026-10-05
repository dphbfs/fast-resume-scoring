# Final set: job picks (pre-registered 2026-10-05, before any scoring)

Held-out resumes: three synthetic (`testdata/resumes/final-syn-{1,2,3}.md`,
written by a separate agent from `synthetic-resume-prompt.md`) and the real
Contract SWE resume (`final-real-contract.md`, masked; its experience is the
Main resume's, so it tests presentation and eligibility lines rather than a
new candidate). Five jobs each, picked by title from unscored archived
applications with a full posting ≥ 1,500 characters, none in the
development or golden sets: 2 on-target, 2 adjacent, 1 off-target. Where
options were close, the shorter posting was taken. Jev was not run on any
of them. List and kinds: `testdata/final/pairs.json`
(`scripts/import_final.py`).

| Resume | On-target | Adjacent | Off-target |
|---|---|---|---|
| syn-1 full-stack (TS/React) | Bicycle Health, Crisis Text Line (mid-level full stack) | Freestar (JS ad tech, senior), Clever Devices (React Native, senior) | Rare Candy (Go backend) |
| syn-2 QA → Python backend | ARETUM, Engenious (backend) | Voxel51, ComboCurve (senior Python) | Unity (Android) |
| syn-3 senior data/ML | Datadog (senior data engineer), Home Depot (backend ML) | Alteryx (dataplane), Alpaca (market data) | Grove (front end / full stack) |
| Contract SWE (Java backend) | Allstate (Java/Spring Boot), Close (backend platform) | Yendo, Sumsub (senior backend, other stacks) | Stryker (Salesforce) |

The set stays sealed until the frozen final run (plan F1): nothing is fitted
or tuned on it.
