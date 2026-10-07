#!/usr/bin/env python3
"""Score pairs with the recruiter judge (secondary judge for the final eval).

Usage:
  scripts/judge.py [-runs 3] -dir testdata/final all
  scripts/judge.py [-runs 3] -dir testdata/reference <pair-id> [<pair-id> ...]
  scripts/judge.py [-runs 3] -dir testdata/reference -top 20 <e2e-report.json> [...]

The rubric is .claude/skills/recruiter-judge/SKILL.md (front matter
stripped), sent as the system prompt; the user message holds the masked
Resume and the Job Description text. Model and endpoint come from .env
(JUDGE_BASE_URL, JUDGE_API_KEY, JUDGE_MODEL), a different model family from
the reference scorer. Each pair is asked -runs times; its judge score is
the median.

-top N picks the N pairs with the largest mean |Match - reference| over the
given e2e reports (development disagreements), Match recomputed with the
v3 formula.

Writes raw replies to eval/judge/<timestamp>.json (gitignored) and merges
the per-pair medians into <dir>/judge.json (committed). Every quoted
must-have is checked against the Resume text; the share found verbatim is
reported per pair as a hallucination check.
"""

import argparse
import hashlib
import json
import re
import statistics
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

SKILL = Path(".claude/skills/recruiter-judge/SKILL.md")


def env():
    out = {}
    for line in Path(".env").read_text().splitlines():
        if "=" in line and not line.lstrip().startswith("#"):
            k, v = line.split("=", 1)
            out[k.strip()] = v.strip().strip('"')
    return out


def rubric():
    text = SKILL.read_text()
    return re.sub(r"\A---\n.*?\n---\n", "", text, flags=re.DOTALL).strip()


def ask(cfg, system, user):
    body = json.dumps({"model": cfg["JUDGE_MODEL"], "messages": [
        {"role": "system", "content": system}, {"role": "user", "content": user}]}).encode()
    req = urllib.request.Request(cfg["JUDGE_BASE_URL"].rstrip("/") + "/chat/completions", data=body, headers={
        "Authorization": "Bearer " + cfg["JUDGE_API_KEY"], "Content-Type": "application/json"})
    t0 = time.time()
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=300) as resp:
                reply = json.load(resp)
            break
        except OSError as err:
            if attempt == 2:
                return {"error": str(err)}
            time.sleep(2 ** attempt * 5)
    text = reply["choices"][0]["message"]["content"]
    m = re.search(r"\{.*\}", text, re.DOTALL)
    try:
        parsed = json.loads(m.group(0)) if m else None
    except json.JSONDecodeError:
        parsed = None
    return {"seconds": round(time.time() - t0, 1), "usage": reply.get("usage"), "raw": text, "parsed": parsed}


def norm(s):
    return re.sub(r"\s+", " ", re.sub(r"[*_`•\-–—]", " ", s)).strip().lower()


def quote_rate(parsed, resume):
    """Share of non-empty must-have quotes found verbatim (whitespace and
    bullet marks ignored) in the Resume; None when there are none."""
    text = norm(resume)
    quotes = [norm(m.get("quote") or "") for m in (parsed or {}).get("must_haves", [])]
    quotes = [q for q in quotes if q]
    if not quotes:
        return None
    return round(sum(q in text for q in quotes) / len(quotes), 2)


def match_v3(s):
    """Match Score v3 from a report's stored Fit Score and Holistic answers
    (mirrors domain.MatchScore), so reports run before the refit rank pairs
    by today's formula."""
    h = s.get("holistic")
    if s.get("fit") is None or not h:
        return None
    x = 44.2 + 31.7 * s["fit"] / 100 + 46.2 * h.get("responsibilities", 0) - 17.5 * h.get("domain_mismatch", 0)
    return round(min(100, max(0, x * (1 - h.get("blocker", 0)))))


def top_disagreements(reports, n):
    errs = {}
    for path in reports:
        for s in json.loads(Path(path).read_text())["pairs"]:
            m = match_v3(s)
            if m is not None and s.get("reference") is not None:
                errs.setdefault(s["id"], []).append(abs(m - s["reference"]))
    ranked = sorted(errs, key=lambda pid: -statistics.mean(errs[pid]))
    return ranked[:n]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-runs", type=int, default=3)
    ap.add_argument("-dir", default="testdata/final")
    ap.add_argument("-top", type=int, default=0)
    ap.add_argument("-parallel", type=int, default=6)
    ap.add_argument("args", nargs="+")
    a = ap.parse_args()

    cfg = env()
    ref_dir = Path(a.dir)
    pairs = {p["id"]: p for p in json.loads((ref_dir / "pairs.json").read_text())["pairs"]}
    if a.top:
        ids = top_disagreements(a.args, a.top)
    elif a.args == ["all"]:
        ids = list(pairs)
    else:
        ids = a.args

    system = rubric()
    jobs = []
    for pid in ids:
        p = pairs[pid]
        user = ("RESUME:\n" + Path(p["resume"]).read_text() +
                "\n\nJOB POSTING:\n" + (ref_dir / p["jd"]).read_text())
        jobs += [(pid, i, user) for i in range(a.runs)]
    with ThreadPoolExecutor(max_workers=a.parallel) as pool:
        replies = list(pool.map(lambda j: (j[0], ask(cfg, system, j[2])), jobs))

    stamp = time.strftime("%Y-%m-%dT%H-%M-%SZ", time.gmtime())
    raw = {"model": cfg["JUDGE_MODEL"], "runs": a.runs, "dir": a.dir, "pairs": {}}
    for pid, r in replies:
        raw["pairs"].setdefault(pid, []).append(r)
    path = Path("eval/judge", stamp + ".json")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(raw, indent=2, ensure_ascii=False) + "\n")

    out_path = ref_dir / "judge.json"
    out = json.loads(out_path.read_text()) if out_path.exists() else {"pairs": []}
    out.update({"judge": "recruiter-judge skill", "model": cfg["JUDGE_MODEL"], "runs": a.runs,
                "rubric_sha256": hashlib.sha256(system.encode()).hexdigest()[:12]})
    merged = {p["pair"]: p for p in out["pairs"]}
    failed = 0
    for pid, rs in raw["pairs"].items():
        resume = Path(pairs[pid]["resume"]).read_text()
        scores = [r["parsed"]["score"] for r in rs if r.get("parsed") and isinstance(r["parsed"].get("score"), int)]
        if not scores:
            failed += 1
            print(f"{pid}: no valid reply")
            continue
        rates = [q for q in (quote_rate(r["parsed"], resume) for r in rs if r.get("parsed")) if q is not None]
        merged[pid] = {"pair": pid, "score": statistics.median(scores), "scores": scores,
                       "quote_rate": round(statistics.mean(rates), 2) if rates else None,
                       "judged_on": stamp[:10]}
        print(f"{pairs[pid]['company'] if 'company' in pairs[pid] else pairs[pid]['title']:30.30s} "
              f"median {merged[pid]['score']:5.1f}  runs {scores}  quotes found {merged[pid]['quote_rate']}")
    out["pairs"] = sorted(merged.values(), key=lambda p: p["pair"])
    out_path.write_text(json.dumps(out, indent=2) + "\n")
    print(f"\nraw: {path}\nmerged: {out_path} ({len(out['pairs'])} pairs, {failed} failed)")


if __name__ == "__main__":
    main()
