#!/usr/bin/env python3
"""Import reference match scores from a job-tracker `list_applications` dump.

Usage: scripts/import_reference.py <dump.json> [out_dir]

The dump is the JSON array returned by the job-tracker MCP tool
`list_applications` (includeArchived=true). Each application with a saved
match score becomes a reference pair: its Job Description, the linked
Resume, and the score. Selection rules (docs/review-v1-plan.md):

  - only the Main resume (other resumes are tailored copies)
  - Job Description of at least MIN_JD_CHARS characters (shorter ones are
    stubs: title, link, location)
  - a single score: applications whose job-agent notes log more than one
    distinct generative score are excluded; no note counts as one

The Job Description is kept as the scorer saw it (the whole field), only
with HTML entities decoded and contacts removed. Excluded applications are
listed with their reason. A fixed, score-stratified subset of SUBSET_SIZE
pairs is marked for tuning runs.

Writes:
  <out_dir>/jd/<id>.txt   the Job Description
  <out_dir>/pairs.json    pairs, exclusions, and counts
"""

import html
import json
import os
import re
import sys
from pathlib import Path

# The tracker ID of the Main resume; private, so it comes from the
# environment rather than the repository.
MAIN_RESUME_ID = os.environ.get("MAIN_RESUME_ID", "")
# Masked copy of the Main resume, as the checker reads it.
MAIN_RESUME_FILE = "testdata/resumes/real-backend.md"
MIN_JD_CHARS = 1000
SUBSET_SIZE = 30
GOLDEN_DIR = Path("testdata/golden")

SCORED_NOTE = re.compile(r"Scored (\S+): \d+ \([^\d)]*(\d+)")
CONTACTS_SECTION = re.compile(r"^#{2,3} Contacts?:?.*?(?=^#{2,3} |\Z)", re.MULTILINE | re.DOTALL)
EMAIL = re.compile(r"[\w.+-]+@[\w-]+(\.[\w-]+)+")
# Phone numbers with separators; bare digit runs are job IDs in URLs.
PHONE = re.compile(r"(?<![\w/=-])(\+\d{1,3}[\s.-])?\(?\d{3}\)?[\s.-]\d{3}[\s.-]\d{4}(?!\w)")
# Job-agent bookkeeping (truncated front matter: query ids, alert thresholds,
# alert_* lines) leaked into a few fields above the "## <company>
# - <role>" title; the posting itself starts at that title.
TITLE = re.compile(r"^## ", re.MULTILINE)


def sanitize(jd: str) -> str:
    jd = html.unescape(jd).replace(" ", " ")
    jd = CONTACTS_SECTION.sub("", jd)
    if m := TITLE.search(jd):
        jd = jd[m.start():]
    jd = EMAIL.sub("[email removed]", jd)
    jd = PHONE.sub("[phone removed]", jd)
    jd = re.sub(r"\n{3,}", "\n\n", jd)
    return jd.strip() + "\n"


def exclusion(app: dict, scores: list[int]) -> str | None:
    if app.get("resumeId") != MAIN_RESUME_ID:
        return "not_main_resume"
    if len(app.get("jobDescription") or "") < MIN_JD_CHARS:
        return "stub_jd"
    if len(set(scores)) > 1:
        return "rescored"
    return None


def stratified(pairs: list[dict], n: int) -> set[str]:
    """Picks n pairs evenly spaced over the score order."""
    ordered = sorted(pairs, key=lambda p: (p["score"], p["id"]))
    if len(ordered) <= n:
        return {p["id"] for p in ordered}
    return {ordered[round(i * (len(ordered) - 1) / (n - 1))]["id"] for i in range(n)}


def main() -> int:
    if not MAIN_RESUME_ID:
        sys.exit("set MAIN_RESUME_ID to the Main resume's ID in the job tracker")
    if len(sys.argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    apps = json.loads(Path(sys.argv[1]).read_text())
    out = Path(sys.argv[2] if len(sys.argv) > 2 else "testdata/reference")
    (out / "jd").mkdir(parents=True, exist_ok=True)
    golden = {p.name.split(".")[0] for p in GOLDEN_DIR.glob("*.txt")}

    pairs, excluded = [], []
    for app in apps:
        if app.get("matchScore") is None:
            continue
        notes = SCORED_NOTE.findall(app.get("notes") or "")
        scores = [int(s) for _, s in notes]
        reason = exclusion(app, scores)
        if reason:
            excluded.append({"id": app["id"], "reason": reason, "scores": scores or [app["matchScore"]]})
            continue
        (out / "jd" / f"{app['id']}.txt").write_text(sanitize(app["jobDescription"]))
        ai = (app.get("aiMetadata") or {}).get("matchScore") or {}
        pairs.append({
            "id": app["id"],
            "title": app.get("role"),
            "company": app.get("company"),
            "resume": MAIN_RESUME_FILE,
            "jd": f"jd/{app['id']}.txt",
            "score": app["matchScore"],
            "strengths": ai.get("strengths", []),
            "gaps": ai.get("gaps", []),
            "golden": app["id"] in golden,
        })

    subset = stratified(pairs, SUBSET_SIZE)
    for p in pairs:
        p["subset"] = p["id"] in subset
    pairs.sort(key=lambda p: p["id"])
    excluded.sort(key=lambda e: (e["reason"], e["id"]))

    counts: dict[str, int] = {"pairs": len(pairs), "subset": len(subset)}
    for e in excluded:
        counts["excluded_" + e["reason"]] = counts.get("excluded_" + e["reason"], 0) + 1
    doc = {
        "scorer": "Generative model match score (Claude Opus 5)",
        "rules": {"resume": "main", "min_jd_chars": MIN_JD_CHARS, "single_score": True},
        "counts": counts,
        "pairs": pairs,
        "excluded": excluded,
    }
    (out / "pairs.json").write_text(json.dumps(doc, indent=2, ensure_ascii=False) + "\n")
    print(json.dumps(counts), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
