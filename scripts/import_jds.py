#!/usr/bin/env python3
"""Import Job Descriptions from a Reactive Resume `list_applications` dump.

Usage: scripts/import_jds.py <dump.json> [out_dir]

The dump is the JSON array returned by the reactive-resume MCP tool
`list_applications` (includeArchived=true). Only applications whose
jobDescription contains the original posting under "### Full Job Description"
are imported; Hermes-condensed descriptions are skipped because they are
derived text, not a real Job Description.

For each imported application this writes:
  <out_dir>/<id>.txt   the sanitized Job Description (title on the first line)
  <out_dir>/<id>.json  metadata sidecar (source, date, title, company, url)
"""

import html
import json
import re
import sys
from pathlib import Path

FULL_MARKER = "### Full Job Description"
MIN_BODY_CHARS = 500

# Lines Hermes or the job aggregator injected into the posting.
INJECTED_LINE = re.compile(
    r"^\s*(company sponsorship flag:|visa sponsorship signal:|treat as a small scoring bonus)",
    re.IGNORECASE,
)
CONTACT_SECTION = re.compile(r"^### Contact:.*?(?=^#{2,3} |\Z)", re.MULTILINE | re.DOTALL)
EMAIL = re.compile(r"[\w.+-]+@[\w-]+(\.[\w-]+)+")
PHONE = re.compile(r"(?<!\w)\+?\d[\d\s().-]{8,}\d(?!\w)")


def sanitize(body: str) -> str:
    # Hermes appends its own "## ..." sections (e.g. "## Contacts") after the
    # posting; the posting body itself has no markdown headings.
    body = re.split(r"^## ", body, maxsplit=1, flags=re.MULTILINE)[0]
    # Greenhouse postings carry HTML entities (&nbsp;, &amp;, &mdash;).
    body = html.unescape(body).replace("\u00a0", " ")
    body = CONTACT_SECTION.sub("", body)
    lines = [ln for ln in body.splitlines() if not INJECTED_LINE.match(ln)]
    body = "\n".join(lines)
    body = EMAIL.sub("[email removed]", body)
    body = PHONE.sub("[phone removed]", body)
    body = re.sub(r"\n{3,}", "\n\n", body)
    return body.strip() + "\n"


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    dump = Path(sys.argv[1])
    out = Path(sys.argv[2] if len(sys.argv) > 2 else "testdata/jd")
    out.mkdir(parents=True, exist_ok=True)

    apps = json.loads(dump.read_text())
    written = skipped = 0
    for app in apps:
        jd = app.get("jobDescription") or ""
        if FULL_MARKER not in jd:
            skipped += 1
            continue
        body = sanitize(jd.split(FULL_MARKER, 1)[1])
        if len(body) < MIN_BODY_CHARS:
            skipped += 1
            continue

        title = f"{app.get('role') or ''}".strip()
        (out / f"{app['id']}.txt").write_text(f"{title}\n\n{body}")
        meta = {
            "id": app["id"],
            "title": title,
            "company": app.get("company"),
            "source": app.get("source"),
            "source_url": app.get("sourceUrl"),
            "date": (app.get("createdAt") or "")[:10],
        }
        (out / f"{app['id']}.json").write_text(json.dumps(meta, indent=2) + "\n")
        written += 1

    print(f"imported {written}, skipped {skipped}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
