# Security policy

## Reporting a vulnerability

Please do not open a public issue. Report it privately through GitHub:
[Security → Report a vulnerability](https://github.com/dphbfs/fast-resume-scoring/security/advisories/new).
Only the maintainers see the report. Expect a first reply within a week.

Include what you found, how to reproduce it, and the version or commit.

## Supported versions

Only the latest release and `main` get fixes.

## What this tool sends where

Treat Resumes and Job Descriptions as personal data. A run sends them to:

- **Jev** (`TYPESAFE_BASE_URL`: TypeSafe or OpenRouter), for every
  command.
- **The generative model** (`OPENAI_BASE_URL`), only when `OPENAI_MODEL` is
  set: `extract` sends the Job Description to write the Job Summary, and
  `eval -checker -baseline` sends the Resume and Job Description.

With `JEV_REPLAY` set, nothing goes to Jev (`extract` still calls the
generative model if `OPENAI_MODEL` is set). The Go commands send nothing
else and have no telemetry. The research
scripts in `scripts/` call the endpoints their docstrings name
(`OPENAI_*`, `JUDGE_*`).

## Handling API keys

- Keep keys in `.env` (gitignored; start from `.env.example`). Never commit
  them, and never paste them into issues or traces.
- Provider errors are sanitized before they are logged, so keys and
  request bodies do not end up in logs. If you see a key or a Resume in
  log output, report it as a vulnerability.
- Output and trace files are written with private permissions (`0600`),
  because they quote the Resume and Job Description.
