#!/usr/bin/env python3
"""Ask the generative reference scorer to explain its match scores.

Usage: scripts/explain_reference.py <runs> <pair-id> [<pair-id> ...]
       scripts/explain_reference.py eval/explain/<run>.json   (summarize again)

Sends Reactive Resume's match-score prompt (verbatim, as in
internal/adapter/eval/baseline.go) plus a request for the factors behind
the score, <runs> times per pair, through the OpenAI-compatible endpoint in
.env (OPENAI_BASE_URL, OPENAI_API_KEY, OPENAI_MODEL). Pairs come from
testdata/reference; the Resume is the masked Markdown copy (Reactive
Resume sends its JSON). Writes the raw replies to
eval/explain/<timestamp>.json and prints a per-pair factor summary.
"""

import json
import os
import re
import sys
import time
import urllib.request
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

# Reactive Resume's prompt (packages/api/src/features/applications/ai.ts).
BASE_PROMPT = (
    "Compare this resume against the job description. Return ONLY JSON with keys score (integer 0-100 fit), "
    "gaps (array of short missing-qualification strings), strengths (array of short matching-strength "
    "strings).\n\nRESUME:\n%s\n\nJOB DESCRIPTION:\n%s"
)

CATEGORIES = [
    "core_technology", "secondary_technology", "role_responsibilities", "seniority_or_years",
    "domain_or_industry", "leadership_or_mentoring", "eligibility_or_location", "education",
    "soft_skills", "other",
]

EXPLAIN = (
    "\n\nAlso add the key \"factors\": an array listing every consideration that moved the score up or down, "
    "each an object with keys factor (short name), category (one of: " + ", ".join(CATEGORIES) + "), "
    "direction (positive or negative), weight (integer 1-10: how much it moved the score), and evidence "
    "(the short resume or job description detail behind it)."
)


def env():
    out = {}
    for line in Path(".env").read_text().splitlines():
        if "=" in line and not line.lstrip().startswith("#"):
            k, v = line.split("=", 1)
            out[k.strip()] = v.strip().strip('"')
    return out


def ask(cfg, prompt):
    body = json.dumps({"model": cfg["OPENAI_MODEL"], "messages": [{"role": "user", "content": prompt}]}).encode()
    req = urllib.request.Request(cfg["OPENAI_BASE_URL"].rstrip("/") + "/chat/completions", data=body, headers={
        "Authorization": "Bearer " + cfg["OPENAI_API_KEY"], "Content-Type": "application/json"})
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=300) as resp:
        reply = json.load(resp)
    text = reply["choices"][0]["message"]["content"]
    m = re.search(r"\{.*\}", text, re.DOTALL)
    return {"seconds": round(time.time() - t0, 1), "usage": reply.get("usage"), "raw": text,
            "parsed": json.loads(m.group(0)) if m else None}


def factors(parsed):
    """The factor list; some replies name the key key_factors."""
    parsed = parsed or {}
    return parsed.get("factors") or parsed.get("key_factors") or []


def summarize(out, path):
    runs = out["runs"]
    for p in out["pairs"].values():
        scores = [r["parsed"]["score"] for r in p["replies"] if r["parsed"]]
        print(f"\n## {p['title']} (reference {p['reference']}): scores {scores}")
        by_cat = defaultdict(list)
        for k, r in enumerate(p["replies"]):
            for f in factors(r["parsed"]):
                sign = 1 if f.get("direction") == "positive" else -1
                by_cat[(f.get("category"), f.get("direction"))].append((k, sign * int(f.get("weight", 0)), f.get("factor")))
        for (cat, d), fs in sorted(by_cat.items(), key=lambda kv: -sum(abs(w) for _, w, _ in kv[1])):
            in_runs = len({k for k, _, _ in fs})
            total = [sum(w for k2, w, _ in fs if k2 == k) for k in range(runs)]
            print(f"  {d:8s} {cat:24s} runs {in_runs}/{runs}  weight per run {total}  e.g. {fs[0][2]}")
    print(f"\nraw: {path}")


def main():
    if sys.argv[1].endswith(".json"):
        summarize(json.loads(Path(sys.argv[1]).read_text()), sys.argv[1])
        return
    runs, ids = int(sys.argv[1]), sys.argv[2:]
    cfg = env()
    pairs = {p["id"]: p for p in json.loads(Path("testdata/reference/pairs.json").read_text())["pairs"]}
    current = {p["pair"]: p["score"] for p in json.loads(Path("testdata/reference/current.json").read_text())["pairs"]}
    jobs = []
    for pid in ids:
        p = pairs[pid]
        prompt = BASE_PROMPT % (Path(p["resume"]).read_text(), Path("testdata/reference", p["jd"]).read_text()) + EXPLAIN
        jobs += [(pid, i, prompt) for i in range(runs)]
    with ThreadPoolExecutor(max_workers=4) as pool:
        replies = list(pool.map(lambda j: (j[0], j[1], ask(cfg, j[2])), jobs))

    out = {"model": cfg["OPENAI_MODEL"], "runs": runs, "pairs": {}}
    for pid, i, r in replies:
        out["pairs"].setdefault(pid, {"title": pairs[pid]["title"], "reference": current.get(pid), "replies": []})
        out["pairs"][pid]["replies"].append(r)
    path = Path("eval/explain", time.strftime("%Y-%m-%dT%H-%M-%SZ", time.gmtime()) + ".json")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(out, indent=2, ensure_ascii=False) + "\n")

    summarize(out, path)


if __name__ == "__main__":
    main()
