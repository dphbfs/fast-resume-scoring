#!/usr/bin/env python3
"""Three-way agreement: Jev's Match Score, the reference, the recruiter judge.

Usage: scripts/compare_judge.py [-dir testdata/final] [<e2e-report.json> ...]

Reads <dir>/current.json (reference), <dir>/judge.json (judge medians), and
any e2e reports (Jev Match per pair, averaged over the reports; reports
from before Match Score v4 are recomputed with the v3 formula). Prints
each pair and, per comparison, MAE, bias, share within 10, Pearson, and
Kendall tau-b over the pairs both sides scored. The judge is a second opinion, not ground truth: if Jev
disagrees with the reference where the judge agrees with Jev, the gap is
less likely to be Jev's error.
"""

import argparse
import json
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from judge import match_v3  # noqa: E402


def tau_b(x, y):
    conc = disc = tx = ty = 0
    for i in range(len(x)):
        for j in range(i + 1, len(x)):
            dx, dy = x[i] - x[j], y[i] - y[j]
            if dx == 0 and dy == 0:
                continue
            if dx == 0:
                tx += 1
            elif dy == 0:
                ty += 1
            elif (dx > 0) == (dy > 0):
                conc += 1
            else:
                disc += 1
    d = ((conc + disc + tx) * (conc + disc + ty)) ** 0.5
    return (conc - disc) / d if d else float("nan")


def agree(name, a, b):
    ids = sorted(set(a) & set(b))
    if len(ids) < 2:
        print(f"{name:22s} n={len(ids)}")
        return
    x, y = [a[i] for i in ids], [b[i] for i in ids]
    err = [p - q for p, q in zip(x, y)]
    r = statistics.correlation(x, y) if len(set(x)) > 1 and len(set(y)) > 1 else float("nan")
    print(f"{name:22s} n={len(ids):2d}  MAE {statistics.mean(abs(e) for e in err):5.1f}  "
          f"bias {statistics.mean(err):+5.1f}  within10 {100 * sum(abs(e) <= 10 for e in err) / len(err):3.0f}%  "
          f"pearson {r:.2f}  tau-b {tau_b(x, y):.2f}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-dir", default="testdata/final")
    ap.add_argument("reports", nargs="*")
    a = ap.parse_args()
    d = Path(a.dir)

    def load(name):
        p = d / name
        return {r["pair"]: r["score"] for r in json.loads(p.read_text())["pairs"]} if p.exists() else {}

    ref, judge = load("current.json"), load("judge.json")
    runs = {}
    for path in a.reports:
        for s in json.loads(Path(path).read_text())["pairs"]:
            # v4 reports carry the Match Score; older ones are recomputed
            # with the v3 formula so pre-refit runs rank by it.
            m = s.get("match") if "role_match" in (s.get("holistic") or {}) else match_v3(s)
            if m is not None:
                runs.setdefault(s["id"], []).append(m)
    jev = {k: statistics.mean(v) for k, v in runs.items()}

    pairs = {p["id"]: p for p in json.loads((d / "pairs.json").read_text())["pairs"]}
    ids = [i for i in pairs if i in judge or i in ref or i in jev]
    print(f"{'pair':34s} {'kind':5s} {'ref':>4s} {'judge':>5s} {'jev':>5s}")
    fmt = lambda v: "–" if v is None else f"{v:.0f}"  # noqa: E731
    for i in ids:
        p = pairs[i]
        label = (p.get("company") or p["title"])[:20] + " / " + Path(p["resume"]).stem[-10:]
        print(f"{label:34.34s} {p.get('kind', ''):5s} {fmt(ref.get(i)):>4s} {fmt(judge.get(i)):>5s} {fmt(jev.get(i)):>5s}")
    print()
    agree("jev vs reference", jev, ref)
    agree("judge vs reference", judge, ref)
    agree("jev vs judge", jev, judge)


if __name__ == "__main__":
    main()
