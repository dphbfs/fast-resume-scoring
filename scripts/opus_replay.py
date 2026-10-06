#!/usr/bin/env python3
"""Replay the generative reference prompt to measure its cost and latency.

Usage: scripts/opus_replay.py [-dir testdata/final2] [-parallel 4]

Sends the reference scorer's prompt (verbatim, as in
scripts/explain_reference.py) with each pair's Markdown resume and Job
Description through the OpenAI-compatible endpoint in .env (OPENAI_*,
the reference model). Records wall time, output tokens, and the score.
The proxy does not report input tokens, so input tokens are estimated as
characters / 3.8 (English prose and Markdown); the reference tool itself
sends the resume as JSON, which is larger, so this is a lower bound.
Prices at $5 / $25 per M input / output tokens (Opus 5). Writes
eval/replay/<timestamp>.json (gitignored) and prints a summary.
"""

import argparse
import json
import statistics
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from explain_reference import BASE_PROMPT, env  # noqa: E402

PRICE_IN, PRICE_OUT = 5.0, 25.0
CHARS_PER_TOKEN = 3.8


def ask(cfg, prompt):
    import re
    import urllib.request
    body = json.dumps({"model": cfg["OPENAI_MODEL"], "messages": [{"role": "user", "content": prompt}]}).encode()
    req = urllib.request.Request(cfg["OPENAI_BASE_URL"].rstrip("/") + "/chat/completions", data=body, headers={
        "Authorization": "Bearer " + cfg["OPENAI_API_KEY"], "Content-Type": "application/json"})
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=300) as resp:
        reply = json.load(resp)
    secs = time.time() - t0
    text = reply["choices"][0]["message"]["content"]
    m = re.search(r"\{.*\}", text, re.DOTALL)
    score = json.loads(m.group(0)).get("score") if m else None
    out_tokens = (reply.get("usage") or {}).get("completion_tokens") or 0
    return {"seconds": round(secs, 2), "output_tokens": out_tokens, "score": score}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-dir", default="testdata/final2")
    ap.add_argument("-parallel", type=int, default=4)
    a = ap.parse_args()
    cfg = env()
    d = Path(a.dir)
    pairs = json.loads((d / "pairs.json").read_text())["pairs"]
    ref = {p["pair"]: p["score"] for p in json.loads((d / "current.json").read_text())["pairs"]}
    jobs = []
    for p in pairs:
        prompt = BASE_PROMPT % (Path(p["resume"]).read_text(), (d / p["jd"]).read_text())
        jobs.append((p["id"], prompt))
    t0 = time.time()
    with ThreadPoolExecutor(max_workers=a.parallel) as pool:
        replies = list(pool.map(lambda j: (j[0], len(j[1]), ask(cfg, j[1])), jobs))
    wall = time.time() - t0

    rows = []
    for pid, chars, r in replies:
        tin = chars / CHARS_PER_TOKEN
        cost = (tin * PRICE_IN + r["output_tokens"] * PRICE_OUT) / 1e6
        rows.append({"pair": pid, "input_tokens_est": round(tin), "cost_usd_est": round(cost, 5), "reference": ref.get(pid), **r})
    secs = sorted(r["seconds"] for r in rows)
    diffs = [abs(r["score"] - r["reference"]) for r in rows if r["score"] is not None and r["reference"] is not None]
    summary = {
        "model": cfg["OPENAI_MODEL"], "pairs": len(rows), "parallel": a.parallel, "wall_seconds": round(wall, 1),
        "seconds_mean": round(statistics.mean(secs), 1), "seconds_p50": secs[len(secs) // 2],
        "seconds_p95": secs[max(0, int(len(secs) * 0.95) - 1)],
        "output_tokens_mean": round(statistics.mean(r["output_tokens"] for r in rows)),
        "input_tokens_est_mean": round(statistics.mean(r["input_tokens_est"] for r in rows)),
        "cost_usd_est_mean": round(statistics.mean(r["cost_usd_est"] for r in rows), 4),
        "rescore_vs_reference_mae": round(statistics.mean(diffs), 1) if diffs else None,
    }
    path = Path("eval/replay", time.strftime("%Y-%m-%dT%H-%M-%SZ", time.gmtime()) + ".json")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"summary": summary, "pairs": rows}, indent=1) + "\n")
    print(json.dumps(summary, indent=1))
    print(path)


if __name__ == "__main__":
    main()
