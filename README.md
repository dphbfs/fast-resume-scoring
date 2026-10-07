# fast-resume-scoring

**Score how well a resume fits a job in under a second, for about $0.0001,
with a typed classifier instead of a frontier-model prompt. Then see which
requirements the resume covers and which it misses.**

[![CI](https://github.com/dphbfs/fast-resume-scoring/actions/workflows/ci.yml/badge.svg)](https://github.com/dphbfs/fast-resume-scoring/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/dphbfs/fast-resume-scoring/graph/badge.svg)](https://codecov.io/gh/dphbfs/fast-resume-scoring)
[![Go Report Card](https://goreportcard.com/badge/github.com/dphbfs/fast-resume-scoring)](https://goreportcard.com/report/github.com/dphbfs/fast-resume-scoring)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/dphbfs/fast-resume-scoring/badge)](https://scorecard.dev/viewer/?uri=github.com/dphbfs/fast-resume-scoring)
[![Release](https://img.shields.io/github/v/release/dphbfs/fast-resume-scoring?sort=semver)](https://github.com/dphbfs/fast-resume-scoring/releases/latest)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Held-out MAE vs Opus 5](https://img.shields.io/badge/held--out_MAE_vs_Opus_5-8.6_pts-informational)](docs/final-report-v1.md)

The usual way to score a resume against a job is one long prompt to a
frontier model: a few cents and 15 seconds per pair, and an answer you have
to parse. This project asks [Jev](https://docs.typesafe.ai/introduction),
TypeSafe's System One model, a few narrow typed questions instead, and
combines the probabilities in code.

On a held-out set it never saw during tuning, the score agrees with a
frontier generative model (Claude Opus 5) as closely as an independent
frontier-model judge does, at **≥ 300× lower cost and ~18× lower latency**.

```console
$ score -jd job.txt -resume resume.md -q
{
  "schema_version": "1",
  "model": "typesafe/jev-1.13-20260917",
  "tuning": "103d7236789f",
  "match_score": 36,
  "signals": {
    "role_match": 0.7625,
    "experience_short": 0.98,
    "blocker": 0.11
  }
}
```

*A new-grad backend resume against a senior Go role: right kind of role
(`role_match` 0.76), clearly short on experience (`experience_short` 0.98).
One Jev request, 0.43 s, $0.00007.*

## Features

- **`score`**: a 0–100 Match Score from one Jev request, with the three
  signals behind it.
- **`extract`**: turns a job description into atomic Requirements ("Go",
  "Kubernetes", "5+ years backend"), each with its Tier
  (required / preferred / mentioned), Importance, alternatives ("Go or
  Ruby"), and the sentences it came from.
- **`check`**: links resume lines to those Requirements and reports
  Coverage (strong / partial / weak / none), the Gaps in required items,
  and a Fit Score, with the evidence quoted.
- **Explainable by design**: `extract` and `check` can write a trace with
  every question asked, its probabilities, and the accept/reject decision.
- **Built to be tuned and measured**: every prompt and weight lives in one
  YAML file, prompts are pinned by a snapshot test, and `eval` scores the
  pipeline against hand-labeled data and reference scores.
- **Production basics**: bounded concurrency and retries, a run deadline,
  input limits, structured logs, a per-run metrics summary (tokens, cost,
  latency p50/p95), sanitized provider errors, private output files.

## Try it without a key

[`examples/`](examples/) holds a synthetic posting and resume, the real
output of every command, and the Jev recording behind it. Replay it
offline:

```sh
git clone https://github.com/dphbfs/fast-resume-scoring && cd fast-resume-scoring
make build
JEV_REPLAY=examples/jev-recording.json OPENAI_MODEL= \
  bin/score -jd examples/job.md -resume examples/resume.md
```

Your own files need a key: a replay only answers the requests it recorded.

## Quickstart

You need Go 1.27.1+ and a Jev API key, either from
[TypeSafe](https://docs.typesafe.ai/introduction) or from
[OpenRouter](https://openrouter.ai).

```sh
go install github.com/dphbfs/fast-resume-scoring/cmd/score@latest
go install github.com/dphbfs/fast-resume-scoring/cmd/extract@latest
go install github.com/dphbfs/fast-resume-scoring/cmd/check@latest

export TYPESAFE_API_KEY=...                      # TypeSafe key, or:
# export TYPESAFE_BASE_URL=https://openrouter.ai/api TYPESAFE_API_KEY=<OpenRouter key>

score -jd job.txt -resume resume.md
```

Or with Docker (linux/amd64 and arm64; the binaries are `score`,
`extract`, and `check`):

```sh
docker run --rm -e TYPESAFE_API_KEY -v "$PWD:/work" \
  ghcr.io/dphbfs/fast-resume-scoring score -jd job.txt -resume resume.md

# no key: the bundled examples and their recording
docker run --rm -e JEV_REPLAY=/examples/jev-recording.json \
  ghcr.io/dphbfs/fast-resume-scoring score -jd /examples/job.md -resume /examples/resume.md
```

Or from a clone: `cp .env.example .env`, fill it in, `make build`, and run
`bin/score`, `bin/extract`, `bin/check`.

### Explain a score

```sh
extract -o requirements.json job.txt
check -requirements requirements.json -resume resume.md -o coverage.json
```

```jsonc
// coverage.json (trimmed, illustrative values)
{
  "requirements": [
    { "id": "req_1", "value": "Kubernetes", "tier": "required",
      "coverage": "partial",
      "evidence": [ { "unit": "e4", "strength": "partial", "p": 0.71 } ] }
  ],
  "fit": { "score": 57, "by_tier": { "required": 58, "preferred": 50 }, "gaps": ["req_5"] },
  "evidence_units": { "e4": { "text": "Migrated 30 services to EKS ...", "role": "Backend Engineer" } }
}
```

Add `-debug trace.json` to `extract` or `check` to see every question and
probability.

### Input format

Plain text or Markdown. Job descriptions need no structure. Resumes parse
best in this convention (anything else still works, with blank metadata):

```markdown
# Experience
## Backend Engineer | Acme | 2021-03 – 2024-06
- Built the billing service in Go on Kubernetes.

# Skills
Languages: Go, Python, SQL
```

Each file may be up to 48 KiB.

## How it works

`score` asks Jev three questions about the whole posting and resume in one
request:

| Signal | Type | Question |
|---|---|---|
| `role_match` | Score, 5 levels | How closely does the candidate's kind of role match this job's? |
| `experience_short` | yes/no | Clearly fewer years, or a clearly lower level, than the job asks for? |
| `blocker` | yes/no | A stated status condition (student, clearance, license, citizenship) clearly unmet? |

`match = (80.7 + 18.5·role_match − 55.2·experience_short) × (1 − blocker)`,
clamped to 0–100. The weights are fitted to generative reference scores
and live in [`tuning/tuning.yaml`](tuning/tuning.yaml).

`extract` and `check` run a longer pipeline of narrow Jev questions:
section labeling, candidate selection, refinement, retrieval, and
gate-then-grade evidence checks. See
[docs/architecture.md](docs/architecture.md).

## Results

Reference: one Claude Opus 5 match-score call per pair (the generative
model approach). Second judge: an independent recruiter-style rubric on a
different model family (3 runs, median). 95% intervals from 2,000 pair
bootstraps.

| Set | Pairs | MAE vs reference | Kendall τ-b | Within ±10 |
|---|---|---|---|---|
| Development | 50 | 5.0 | 0.83–0.88 | 87–90% |
| **Held out (sealed)** | 20 | **8.6** [6.0, 11.1] | **0.78** [0.63, 0.92] | 60% |
| *Independent judge vs reference, same held-out set* | 20 | *8.8* | *0.78* | |

| | Jev `score` | Opus 5 prompt |
|---|---|---|
| Cost per score | $0.0001 | ≥ $0.033 |
| Latency, mean | 0.3 s in process, 0.8 s CLI | 14.1 s (p95 20.4 s) |
| 20 pairs, 4 in parallel | ~2 s | 76 s |

The generative reference is itself not fixed: the same pairs scored days
apart moved by +11.6 points on average, and the same resume as Markdown
instead of JSON scored 14.9 points lower. Full method and numbers:
[docs/final-report-v1.md](docs/final-report-v1.md).

## Limitations

- **Small evaluation**: 70 scored pairs and 9 resumes in total; the
  intervals are wide.
- **Career changers are under-scored** on roles in their new field.
- **Off-role or location-restricted postings** can be over-scored by
  10–15 points. Location is deliberately not a blocker.
- **English only**, tested on software, data, and security roles.
- The score is a screening signal, not a hiring decision.

## Configuration

All settings are environment variables; see [`.env.example`](.env.example)
for the full list with defaults. The ones you are likely to touch:

| Variable | Default | Purpose |
|---|---|---|
| `TYPESAFE_API_KEY` | (required) | Jev key (TypeSafe or OpenRouter) |
| `TYPESAFE_BASE_URL` | `https://api.typesafe.ai` | `https://openrouter.ai/api` for OpenRouter |
| `JEV_MODEL` | `jev-latest` | Pin a version, e.g. `jev-1.13` |
| `JEV_MAX_CONCURRENCY` | `8` | Parallel Jev requests |
| `OPENAI_BASE_URL` / `OPENAI_API_KEY` / `OPENAI_MODEL` | unset | Optional generative model for `extract`'s Job Summary |
| `RUN_DEADLINE` | `120s` | Whole-run limit |
| `TUNING_FILE` | built-in | Your edited copy of `tuning/tuning.yaml` |
| `JEV_RECORD` / `JEV_REPLAY` | unset | Save every Jev exchange to a file / answer from it with no key or network |

## Documentation

- [Architecture](docs/architecture.md): pipeline and code layout
- [CONTEXT.md](CONTEXT.md): glossary of domain terms
- [Decision records](docs/adr/)
- [Tuning log](docs/tuning.md): every experiment, including the ones that
  lost
- [Final evaluation report](docs/final-report-v1.md)

## Contributing

Issues, eval fixtures, and pull requests are welcome. Read
[CONTRIBUTING.md](CONTRIBUTING.md) first; scoring changes need measurements
over several runs. Please follow the [Code of Conduct](CODE_OF_CONDUCT.md)
and report security issues privately ([SECURITY.md](SECURITY.md)).

## License

[Apache License 2.0](LICENSE).
