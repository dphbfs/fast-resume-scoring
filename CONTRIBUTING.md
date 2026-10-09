# Contributing

Thanks for helping. Bug reports, eval fixtures, and tuning experiments are
as welcome as code.

## Setup

- Go 1.27.2 or newer (see `go.mod`).
- [golangci-lint](https://golangci-lint.run/) v2 for `make lint` and
  `make fmt` (CI pins the version in `.github/workflows/ci.yml`).
- Python 3.11+ only for the research scripts in `scripts/`.
- A Jev key (TypeSafe or OpenRouter) only for live runs and `make eval*`.
  Unit tests need no key and no network.

```sh
git clone https://github.com/dphbfs/fast-resume-scoring
cd fast-resume-scoring
make all          # wire-check, vet, test, build
```

## Before you open a pull request

```sh
make fmt
make all
make race
make lint
```

CI runs the same checks, plus `go mod tidy`, govulncheck, and CodeQL.

- **`go test` never calls a live API.** Tests use the fake Jev server in
  `internal/adapter/jev/jevtest` with recorded responses. Keep it that way.
- **Dependency injection is generated.** After changing a constructor
  signature, run `make wire` and commit the regenerated `wire_gen.go`
  files. CI runs `make wire-check`.
- **Prompts are pinned.** Every Jev question and score weight lives in
  `tuning/tuning.yaml`. `TestPromptSnapshot` compares the exact requests
  with `internal/app/testdata/prompts.golden.json`. If you change a wording
  on purpose, regenerate it and review the diff:

  ```sh
  go test ./internal/app -run TestPromptSnapshot -update
  ```

  Any change to what is sent to Jev also breaks the example replay
  (`go test ./cmd/...`): re-record with `make examples` (needs a key,
  about $0.006) and commit the new outputs.

  Question wordings are fitted constants. A wording change needs a probe
  (`scripts/probe_holistic.py`) and a refit (`scripts/fit_match.py`) on
  development data, with the results in the pull request.
- **Use the domain terms.** Names in code and docs follow `CONTEXT.md`
  (Requirement, Evidence Unit, Coverage, ...). New concepts get a
  `CONTEXT.md` entry; design decisions get an ADR in `docs/adr/`.
- **Every stage logs and counts.** New pipeline stages log with `slog`,
  report metrics through `port.Metrics`, and send Jev calls through the
  bounded limiter.

## Changing scoring behavior

Jev answers vary slightly between identical runs (about 0.5 points on the
evals), so:

1. Compare configurations on **3 or more runs each** and report mean and
   range. One run is not evidence.
2. Record the experiment in `docs/tuning.md`, including losing variants.
3. Tune on development data only (`testdata/reference`, `testdata/final`).
   Never tune on a sealed set.

Live evals (need `.env`, cost real money, write `eval/reports/`):

| Command | What it measures |
|---|---|
| `make eval` | Requirement Extractor vs golden labels (`testdata/golden`) |
| `make eval-checker` | Resume Checker vs labeled Coverage (`testdata/checker`) |
| `make eval-e2e` | Match Score vs the generative reference scores |

Pass flags with `EVAL_ARGS`, e.g. `make eval EVAL_ARGS="-only 01a0eeca"`.
After editing labels, rescore stored results offline:
`./bin/eval -rescore eval/reports/<run>.json`.

## Test data

- Never commit real personal data. Resumes must be synthetic or masked
  (fake name, contacts, links, companies, schools).
- Label rules: `testdata/golden/README.md` and
  `testdata/checker/README.md`. `go test` lints the golden labels.

## Commits and pull requests

- [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`,
  `fix:`, `docs:`, `test:`, `chore:`).
- One logical change per pull request; say what you measured if it touches
  scoring.
- Add a line to `CHANGELOG.md` under **Unreleased** for user-visible
  changes.

By contributing you agree that your work is licensed under the
[Apache License 2.0](LICENSE) and that you follow the
[Code of Conduct](CODE_OF_CONDUCT.md).
