#!/usr/bin/env python3
"""Calibrate the Fit Score knobs offline against an end-to-end eval report.

Usage: scripts/calibrate_fit.py eval/reports/e2e/<run>.json

Reads each pair's Coverage from <run>-traces/<id>.json and searches a
coarse, constrained grid (docs/review-v1-plan.md, A3):

  credits: none 0, strong 1 (fixed); 0 <= weak <= partial <= 1
  weights: required 3 (fixed); 0 < mentioned <= preferred <= 3

The Fit Score is the Tier-weighted average credit per item, with an
Alternative Group counting once (domain.ScoreFit). Reports the in-sample
best grid point and the leave-one-out MAE (each pair predicted by the grid
point fitted on the other pairs), which is the honest estimate.
"""

import json
import sys
from itertools import product
from pathlib import Path

TIERS = ["required", "preferred", "mentioned"]
STRENGTHS = ["strong", "partial", "weak", "none"]
RANK = {"none": 0, "weak": 1, "partial": 2, "strong": 3}
TIER_RANK = {"required": 2, "preferred": 1}


def items(coverage):
    """Scored items as (tier, coverage), grouping as domain.ScoreFit does."""
    group_of = {}
    for gi, g in enumerate(coverage.get("alternative_groups") or []):
        for m in g["members"]:
            group_of.setdefault(m, gi)
    out, at = [], {}
    for r in coverage["requirements"]:
        tier = r.get("tier") if TIER_RANK.get(r.get("tier"), 0) else "mentioned"
        cov = r["coverage"]
        gi = group_of.get(r["id"])
        if gi is None:
            out.append([tier, cov])
        elif gi not in at:
            at[gi] = len(out)
            out.append([tier, cov])
        else:
            it = out[at[gi]]
            if TIER_RANK.get(tier, 0) > TIER_RANK.get(it[0], 0):
                it[0] = tier
            if RANK[cov] > RANK[it[1]]:
                it[1] = cov
    return out


def counts(coverage):
    """Item counts per (Tier, Coverage), as a 3 x 4 table."""
    n = [[0] * 4 for _ in TIERS]
    for tier, cov in items(coverage):
        n[TIERS.index(tier)][STRENGTHS.index(cov)] += 1
    return n


def score(n, partial, weak, preferred, mentioned):
    """Fit Score of one pair's count table (Go rounds half away from zero)."""
    credit = (1, partial, weak, 0)
    weight = (3, preferred, mentioned)
    earned = sum(weight[t] * credit[c] * n[t][c] for t in range(3) for c in range(4))
    total = sum(weight[t] * sum(n[t]) for t in range(3))
    return int(100 * earned / total + 0.5)


def grid():
    steps = [i / 20 for i in range(21)]
    for partial, weak in product(steps, steps):
        if weak > partial:
            continue
        for preferred in [0.5 + i / 4 for i in range(11)]:
            for mentioned in [i / 4 for i in range(1, int(preferred * 4) + 1)]:
                yield partial, weak, preferred, mentioned


def mean(xs):
    return sum(xs) / len(xs)


def main():
    report = Path(sys.argv[1])
    rep = json.loads(report.read_text())
    traces = report.with_suffix("").as_posix() + "-traces"
    ids, ns, refs = [], [], []
    for p in rep["pairs"]:
        if p.get("error") or p.get("reference") is None:
            continue
        cov = json.loads(Path(traces, p["id"] + ".json").read_text())["coverage"]
        ids.append(p["id"])
        ns.append(counts(cov))
        refs.append(p["reference"])
    points = list(grid())
    err = [[abs(score(n, *g) - r) for n, r in zip(ns, refs)] for g in points]

    current = (0.6, 0.3, 1.5, 1.0)
    cur = [score(n, *current) for n in ns]
    print(f"pairs {len(refs)}, grid points {len(points)}")
    print(f"current {current}: MAE {mean([abs(c - r) for c, r in zip(cur, refs)]):.1f}, "
          f"bias {mean([c - r for c, r in zip(cur, refs)]):+.1f}")
    best = min(range(len(points)), key=lambda g: mean(err[g]))
    pred = [score(n, *points[best]) for n in ns]
    print(f"in-sample best {points[best]}: MAE {mean(err[best]):.1f}, "
          f"bias {mean([p - r for p, r in zip(pred, refs)]):+.1f}")

    loo = []
    for i in range(len(refs)):
        g = min(range(len(points)), key=lambda g: sum(err[g]) - err[g][i])
        loo.append(err[g][i])
    print(f"leave-one-out MAE {mean(loo):.1f} (max {max(loo):.0f})")


if __name__ == "__main__":
    main()
