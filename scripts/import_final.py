#!/usr/bin/env python3
"""Write the final evaluation set's Job Descriptions and pair list.

Usage: scripts/import_final.py <dump.json> [final2]

The dump is a the generative scorer `list_applications` result (includeArchived).
PICKS lists, per held-out resume, the applications chosen by title before
any scoring (docs/final-set/picks.md). Writes testdata/final/jd/<id>.txt
(cleaned as in scripts/import_reference.py) and testdata/final/pairs.json
without scores; scripts/add_final_scores.py fills them in.

With "final2", writes the second sealed set (docs/final-set-2/picks.md,
PICKS2) to testdata/final2. A posting may serve several resumes there, so
its pair IDs are <application id>--<resume file stem>.
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from import_reference import sanitize  # noqa: E402

# resume file -> [(application id suffix, kind)]
PICKS = {
    "testdata/resumes/final-syn-1.md": [
        ("6d45daa3", "on"), ("6feeae7c", "on"), ("bef0317e", "adjacent"), ("049dd2f4", "adjacent"), ("67807255", "off"),
    ],
    "testdata/resumes/final-syn-2.md": [
        ("55597371", "on"), ("a06dddbb", "on"), ("e87886a6", "adjacent"), ("89fce024", "adjacent"), ("9c2ae8c1", "off"),
    ],
    "testdata/resumes/final-syn-3.md": [
        ("ecb765ad", "on"), ("116c0a3d", "on"), ("71f62048", "adjacent"), ("ce69c15e", "adjacent"), ("79e180cb", "off"),
    ],
    "testdata/resumes/final-real-contract.md": [
        ("3d55ca57", "on"), ("8097f4fc", "on"), ("c5e9c4cf", "adjacent"), ("5562cd9f", "adjacent"), ("4b6133c0", "off"),
    ],
}


# Second sealed set (docs/final-set-2/picks.md).
PICKS2 = {
    "testdata/resumes/final2-syn-1.md": [
        ("f8803ae6", "on"), ("14b5aa11", "on"), ("2ca486bf", "adjacent"), ("aa306dc6", "adjacent"), ("e357010c", "off"),
    ],
    "testdata/resumes/final2-syn-2.md": [
        ("ef43993d", "on"), ("6ae31cca", "on"), ("9f1545ff", "adjacent"), ("b7b9984b", "adjacent"), ("18cf5f3b", "off"),
    ],
    "testdata/resumes/final2-syn-3.md": [
        ("af365ec3", "on"), ("b7b9984b", "on"), ("ad38269a", "adjacent"), ("b5ce6cfe", "adjacent"), ("8b1361d4", "off"),
    ],
    "testdata/resumes/final2-real-analyst.md": [
        ("cb29378c", "on"), ("8b1361d4", "on"), ("b13f2d15", "adjacent"), ("78c3fdce", "adjacent"), ("fb4ba38b", "off"),
    ],
}


def main():
    apps = {a["id"]: a for a in json.loads(Path(sys.argv[1]).read_text())}.values()
    second = sys.argv[2:] == ["final2"]
    out = Path("testdata/final2" if second else "testdata/final")
    (out / "jd").mkdir(parents=True, exist_ok=True)
    pairs = []
    for resume, picks in (PICKS2 if second else PICKS).items():
        for suffix, kind in picks:
            (app,) = [a for a in apps if a["id"].endswith(suffix)]
            (out / "jd" / f"{app['id']}.txt").write_text(sanitize(app["jobDescription"]))
            pid = f"{app['id']}--{Path(resume).stem}" if second else app["id"]
            pairs.append({"id": pid, "title": app.get("role"), "company": app.get("company"), "resume": resume,
                          "jd": f"jd/{app['id']}.txt", "kind": kind})
    (out / "pairs.json").write_text(json.dumps({"pairs": pairs}, indent=2, ensure_ascii=False) + "\n")
    print(len(pairs), "pairs")


if __name__ == "__main__":
    main()
