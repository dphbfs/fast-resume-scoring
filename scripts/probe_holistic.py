#!/usr/bin/env python3
"""Ask candidate Holistic Round questions without running the pipeline.

Usage: scripts/probe_holistic.py [-runs 1] [-parallel 8]

Sends one Jev request per pair (state: the posting as the extractor reads
it, plus the Resume, exactly like app.HolisticJudge) with the production
Holistic questions from tuning/tuning.yaml plus the CANDIDATES defined
here, for every development pair: testdata/reference pairs with a
current score (subset) and every testdata/final pair. Writes the answers
to eval/probe/<timestamp>.json (gitignored); scripts/fit_match.py -probe
merges them with the Fit Scores stored in e2e reports.

Jev bills input tokens only and the state dominates, so asking several
questions per request costs about one Holistic Round (~$0.0003 per pair).
"""

import argparse
import json
import os
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import yaml

# Candidate questions under test, asked next to the production ones. The
# ones that lost (responsibilities, domain mismatch, must-haves, the old
# blocker) are in docs/tuning.md, "Redesign round 1".
CANDIDATES = {}


def production_questions():
    """The Holistic Round questions as app.HolisticJudge sends them, built
    from tuning/tuning.yaml (TUNING_FILE overrides it, as for the binaries)."""
    path = Path(os.environ.get("TUNING_FILE") or "tuning/tuning.yaml")
    h = yaml.safe_load(path.read_text())["holistic"]
    noul = lambda q: {"type": "noul", "instructions": q["question"],  # noqa: E731
                      "criteria": {"true": q["if_true"], "false": q["if_false"]}}
    return {
        "role_match": {"type": "score", "instructions": h["role_match"]["question"],
                       "criteria": h["role_match"]["levels"]},
        "experience_short": noul(h["experience_short"]),
        "blocker": noul(h["blocker"]),
    }


QUESTIONS = {**production_questions(), **CANDIDATES}


def env():
    out = {}
    for line in Path(".env").read_text().splitlines():
        if "=" in line and not line.lstrip().startswith("#"):
            k, v = line.split("=", 1)
            out[k.strip()] = v.strip().strip('"')
    return out


def posting_text(text):
    """Mirror app.postingText: keep the title line and the full posting."""
    marker = "\n### Full Job Description\n"
    i = text.find(marker)
    if i < 0:
        return text
    return text.split("\n", 1)[0] + "\n\n" + text[i + len(marker):].lstrip("\n")


def pairs():
    out = []
    ref = Path("testdata/reference")
    current = {p["pair"] for p in json.loads((ref / "current.json").read_text())["pairs"]}
    for p in json.loads((ref / "pairs.json").read_text())["pairs"]:
        if p.get("subset") and p["id"] in current:
            out.append((ref, p))
    fin = Path("testdata/final")
    out += [(fin, p) for p in json.loads((fin / "pairs.json").read_text())["pairs"]]
    return out


def ask(cfg, state):
    body = json.dumps({"model": cfg["JEV_MODEL"], "state": state, "questions": QUESTIONS}).encode()
    req = urllib.request.Request(cfg["TYPESAFE_BASE_URL"].rstrip("/") + "/v1/systemone", data=body, headers={
        "Authorization": "Bearer " + cfg["TYPESAFE_API_KEY"], "Content-Type": "application/json"})
    for attempt in range(4):
        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                return json.load(resp)
        except OSError as err:
            if attempt == 3:
                return {"error": str(err)}
            time.sleep(2 ** attempt * 3)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-runs", type=int, default=1)
    ap.add_argument("-parallel", type=int, default=8)
    a = ap.parse_args()
    cfg = env()
    jobs = []
    for d, p in pairs():
        state = {"job_posting": posting_text((d / p["jd"]).read_text()), "resume": Path(p["resume"]).read_text()}
        jobs += [(p["id"], state)] * a.runs
    with ThreadPoolExecutor(max_workers=a.parallel) as pool:
        replies = list(pool.map(lambda j: (j[0], ask(cfg, j[1])), jobs))
    out = {"questions": QUESTIONS, "pairs": {}}
    cost = 0.0
    for pid, r in replies:
        out["pairs"].setdefault(pid, []).append(r)
        cost += ((r.get("usage") or {}).get("cost") or 0)
    errors = sum(1 for _, r in replies if "error" in r)
    path = Path("eval/probe", time.strftime("%Y-%m-%dT%H-%M-%SZ", time.gmtime()) + ".json")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(out, indent=1) + "\n")
    print(f"{len(replies)} requests, {errors} errors, cost ${cost:.4f}\n{path}")


if __name__ == "__main__":
    main()
