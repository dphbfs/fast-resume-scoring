# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.0.5] - 2026-10-12

### Added

- `resume-scoring-mcp`: an MCP server with `score`, `extract`, and `check`
  tools over stdio or streamable HTTP (`-transport http`, bearer token from
  `MCP_HTTP_TOKEN`, required on non-loopback addresses). Ships in the
  release archives and the Docker image.
- `install.sh`: installs the binaries from a release (checksum-verified,
  cosign-verified when cosign v2.4.1 or newer is installed; older versions
  skip the signature check with a warning) to `~/.local/bin` or
  `--installation-dir`, and the tuning file to
  `~/.config/fast-resume-scoring/`.
- Release archives include `tuning.yaml`; the Docker image has it at
  `/etc/fast-resume-scoring/tuning.yaml`.

### Changed

- README reorganized: install, usage (CLI and MCP), features, results,
  configuration, and tuning, with a demo of Claude Code calling the MCP
  server.
- README opens with a launch demo GIF (`docs/demo/`).
- Doc comments on the exported constants and fields in `internal/domain`.

## [0.0.4] - 2026-10-12

### Security

- Release checksums are signed with Sigstore (`checksums.txt.sigstore.json`);
  see SECURITY.md to verify.
- The release workflow's write permissions are scoped to its job.
- `golang.org/x/mod` 0.20.0 → 0.42.0 (GO-2026-6179, GO-2026-6180; only
  used by the `wire` code generator, not in the binaries).

### Added

- Fuzz tests for the Resume parser, the sentence splitter, and PDF text
  normalization.

### Changed

- The example candidate in `examples/` is renamed (re-recorded; Match
  Score 86 → 87, Coverage and Fit unchanged).
- `scripts/judge.py` and `scripts/probe_holistic.py` read the process
  environment (overriding an optional `.env`) and exit with a clear message
  when a required variable is missing; `.env.example` lists the `JUDGE_*`
  variables.

### Removed

- Internal planning notes and one-off import scripts. Their data stays.

## [0.0.3] - 2026-10-12

### Changed

- Dependency: `golang.org/x/sync` 0.23.0 → 0.24.0.
- CI: `github/codeql-action` 4.38.2 → 4.38.3.

## [0.0.2] - 2026-10-09

### Added

- `score` and `check` accept a PDF Resume (`-resume resume.pdf`). Text is
  extracted locally with `github.com/ledongthuc/pdf` in a child process
  (same binary, 10 s timeout, no API keys in its environment), so a
  malformed PDF fails with an error instead of hanging or crashing the
  command. Known headings and bullet glyphs become the Markdown
  convention. Scanned PDFs are rejected (no OCR).

### Security

- Built with Go 1.27.2, which fixes nine standard-library vulnerabilities
  that govulncheck reported as reachable (net/http HTTP/1 and HTTP/2,
  crypto/tls, net/textproto). Building from source now needs Go 1.27.2 or
  newer.

## [0.0.1] - 2026-10-08

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

[Unreleased]: https://github.com/dphbfs/fast-resume-scoring/compare/v0.0.5...HEAD
[0.0.5]: https://github.com/dphbfs/fast-resume-scoring/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/dphbfs/fast-resume-scoring/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/dphbfs/fast-resume-scoring/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/dphbfs/fast-resume-scoring/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/dphbfs/fast-resume-scoring/releases/tag/v0.0.1
