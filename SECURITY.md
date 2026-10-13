# Security policy

## Reporting a vulnerability

Please do not open a public issue. Report it privately through GitHub:
[Security → Report a vulnerability](https://github.com/dphbfs/fast-resume-scoring/security/advisories/new).
Only the maintainers see the report. Expect a first reply within a week.

Include what you found, how to reproduce it, and the version or commit.

## Supported versions

Only the latest release and `main` get fixes.

## Verifying a release

From v0.0.4, `checksums.txt` in every release is signed keylessly with
Sigstore by the release workflow. Check the signature, then the archive:

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/dphbfs/fast-resume-scoring/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --ignore-missing -c checksums.txt
```

The bundle needs cosign v2.5 or newer; on v2.4.1 to v2.4.x, add
`--new-bundle-format`. Older versions cannot read it. `install.sh` does
this check when cosign is installed, and skips it with a warning on a
version too old to read the bundle (the archive checksum is always
checked).

## What this tool sends where

Treat Resumes and Job Descriptions as personal data. A run sends them to:

- **Jev** (`TYPESAFE_BASE_URL`: TypeSafe or OpenRouter), for every
  command.
- **The generative model** (`OPENAI_BASE_URL`), only when `OPENAI_MODEL` is
  set: `extract` sends the Job Description to write the Job Summary, and
  `eval -checker -baseline` sends the Resume and Job Description.

A PDF Resume is read locally: only its extracted text is sent, like a
text Resume. The PDF parser runs in a child process of the same binary,
with a timeout and with none of your environment variables (so no API
keys), and a malformed file fails that process, not the command.

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
