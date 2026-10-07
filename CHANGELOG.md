# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

First public version.

### Added

- `score`: Match Score (0–100) of a Resume for a Job Description from one
  Jev request (Holistic Round: `role_match`, `experience_short`,
  `blocker`). Match schema v1.
- `extract`: Requirement Extractor. Turns a Job Description into
  Requirements with Tier, Importance, Alternative Groups, and the source
  sentences. Result schema v1, optional `-debug` trace.
- `check`: Resume Checker. Links a Resume's Evidence Units to the
  Requirements and reports Coverage per Requirement, Gaps, and a Fit
  Score. Coverage schema v1, optional `-debug` trace.
- `eval`: live evaluation against golden labels (extractor, checker) and
  against generative reference scores (`-e2e`), with offline `-rescore`.
- `JEV_RECORD` / `JEV_REPLAY`: record Jev exchanges to a file and replay
  them with no API key or network.
- `examples/`: a synthetic posting and resume with the real output of
  every command and the recording behind it; `go test` replays it.
- Docker image `ghcr.io/dphbfs/fast-resume-scoring` (linux/amd64, arm64)
  with the examples bundled; published by the release workflow.
- `docs/eval.md` and an agreement chart for the held-out set.
- Every prompt and score weight in `tuning/tuning.yaml`, embedded in the
  binaries and overridable with `TUNING_FILE`.
- Bounded concurrency in front of Jev, retries, a run deadline, input size
  limits, sanitized provider errors, private atomic output files, structured
  logs, and a per-run metrics summary.

### Fixed

- `score -h`, `extract -h`, and `check -h` print usage without an API key.
