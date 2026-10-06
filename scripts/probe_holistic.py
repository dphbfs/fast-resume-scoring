#!/usr/bin/env python3
"""Ask candidate Holistic Round questions without running the pipeline.

Usage: scripts/probe_holistic.py [-runs 1] [-parallel 8]

Sends one Jev request per pair (state: the posting as the extractor reads
it, plus the Resume, exactly like app.HolisticJudge) with the questions in
QUESTIONS, for every development pair: testdata/reference pairs with a
current score (subset) and every testdata/final pair. Writes the answers
to eval/probe/<timestamp>.json (gitignored); scripts/fit_match.py -probe
merges them with the Fit Scores stored in e2e reports.

Jev bills input tokens only and the state dominates, so asking several
questions per request costs about one Holistic Round (~$0.0003 per pair).
"""

import argparse
import json
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

LEVELS_RESP = [
    "None of the job's day-to-day responsibilities appear in the candidate's work.",
    "A few of them, in a limited way.",
    "About half of them.",
    "Most of them.",
    "Nearly all of them, at the scope the job describes.",
]

QUESTIONS = {
    # The production question, asked again so the probe is self-contained.
    "responsibilities": {
        "type": "score",
        "instructions": "Regardless of product or industry domain, how much of the day-to-day responsibilities "
                        "in `job_posting` (what the person will build, own, and operate) has the candidate in `resume` carried out?",
        "criteria": LEVELS_RESP,
    },
    "domain_mismatch": {
        "type": "noul",
        "instructions": "Is the product or industry domain of `job_posting` (for example security, payments, "
                        "identity, healthcare) one the candidate in `resume` has never worked in?",
        "criteria": {
            "true": "The candidate's work shows no experience in the job's product or industry domain.",
            "false": "The candidate has worked in the job's domain or a closely related one, or the job has no specific domain.",
        },
    },
    "blocker": {
        "type": "noul",
        "instructions": "Does `job_posting` state a hard eligibility condition other than location (for example: must "
                        "be a current student, must hold a security clearance or license) that `resume` clearly shows the "
                        "candidate does not meet?",
        "criteria": {
            "true": "A stated non-location condition is clearly not met by what the resume shows.",
            "false": "No such condition is stated, or the resume does not clearly contradict it (a condition the "
                     "resume does not mention, such as citizenship, is not clearly unmet).",
        },
    },
    # Candidates.
    "blocker_v3": {
        "type": "noul",
        "instructions": "Does `job_posting` state a hard eligibility condition that `resume` clearly shows the candidate "
                        "does not meet? Only status conditions count: must be a current student or recent graduate, must "
                        "hold a security clearance, a professional license, or citizenship the resume rules out. Skills, "
                        "years of experience, degrees, and location are qualifications, not eligibility conditions.",
        "criteria": {
            "true": "A stated status condition (student, clearance, license, citizenship) is clearly not met.",
            "false": "No status condition is stated, it is met, or the resume does not clearly contradict it. "
                     "Missing years, a missing degree, or missing skills are never a yes.",
        },
    },
    "role_match": {
        "type": "score",
        "instructions": "How closely does the kind of role the candidate in `resume` has been doing match the kind "
                        "of role in `job_posting`? Judge the role (what the person builds and which specialty), not "
                        "seniority or domain.",
        "criteria": [
            "A different profession or specialty (for example Android/mobile vs data engineering, Salesforce "
            "development vs backend services, front-end vs data/ML).",
            "A neighboring specialty with little overlap in daily work.",
            "The same broad field with a different focus (for example front-end-leaning vs backend-leaning).",
            "The same kind of role with a different main stack.",
            "The same kind of role and stack.",
        ],
    },
    "must_haves": {
        "type": "score",
        "instructions": "Take the must-have requirements in `job_posting` (its explicit requirements and the core "
                        "work of the role). How many of them does `resume` show the candidate has actually done in "
                        "their work (not only listed as skills)?",
        "criteria": [
            "None or almost none.",
            "A few, and not the central ones.",
            "About half, including some central ones.",
            "Most, including the central ones.",
            "Nearly all.",
        ],
    },
    "experience_short": {
        "type": "noul",
        "instructions": "Does `resume` show clearly fewer years of the relevant kind of experience, or a clearly "
                        "lower career level, than `job_posting` asks for?",
        "criteria": {
            "true": "The candidate's relevant experience or level is clearly below what the job asks for.",
            "false": "The candidate meets or exceeds the experience and level, or is within a year or so of it.",
        },
    },
}


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
