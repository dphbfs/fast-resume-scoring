#!/usr/bin/env python3
"""Record the final set's reference scores.

Usage: scripts/add_final_scores.py <pair-id>=<score> [...]

Each final-set pair was scored once by the generative reference scorer on
an eval-only copy (same Job Description text as testdata/final/jd/<id>.txt,
the pair's held-out Resume), then archived. Writes
testdata/final/current.json in the same shape as
testdata/reference/current.json, so `eval -e2e -e2e-dir testdata/final
-set current` reads it. Refuses unknown pair IDs and, once every pair has a
score, reports any pair still missing.
"""

import json
import sys
import time
from pathlib import Path

DIR = Path("testdata/final")


def main():
    pairs = [p["id"] for p in json.loads((DIR / "pairs.json").read_text())["pairs"]]
    path = DIR / "current.json"
    out = json.loads(path.read_text()) if path.exists() else {
        "scorer": "generative reference match score (Claude Opus 5), current setup",
        "method": "eval-only copies of each pair (same JD text as jd/<id>.txt, the pair's held-out resume), "
                  "scored once, then archived",
        "pairs": [],
    }
    scores = {p["pair"]: p for p in out["pairs"]}
    for arg in sys.argv[1:]:
        pid, score = arg.split("=", 1)
        if pid not in pairs:
            sys.exit(f"unknown pair {pid}")
        scores[pid] = {"pair": pid, "score": int(score), "calls": 1}
    out["scored_on"] = time.strftime("%Y-%m-%d", time.gmtime())
    out["pairs"] = [scores[p] for p in pairs if p in scores]
    path.write_text(json.dumps(out, indent=2) + "\n")
    missing = [p for p in pairs if p not in scores]
    print(f"{len(out['pairs'])}/{len(pairs)} scored" + (f"; missing: {', '.join(missing)}" if missing else ""))


if __name__ == "__main__":
    main()
