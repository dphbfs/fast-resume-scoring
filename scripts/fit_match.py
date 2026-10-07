#!/usr/bin/env python3
"""Refit the Match Score offline and check how it generalizes.

Usage: scripts/fit_match.py [-probe eval/probe/<run>.json] <e2e-report.json> [...]

Pools e2e reports from any set (each report's "dir" or "set" names where
its pairs live; reports without "dir" are testdata/reference). Per pair,
Fit and Holistic answers are averaged over the reports that ran it, so a
pair counts once however many runs it has.

Each model is linear in its features, times (1 - blocker), clamped 0-100,
like domain.MatchScore, and fitted by least squares on
features * (1 - blocker). Reports, per model: in-sample MAE, pair-held-out
MAE (leave one pair out), and resume-held-out MAE (leave one resume out),
the honest estimate when new resumes are the question; plus MAE per
resume group and tau-b.

With -probe, Holistic answers come from scripts/probe_holistic.py instead
(averaged over its runs; Score answers as a share of the top level), and
the probe's questions are available as features; Fit still comes from
the reports. Only pairs present in both are used.
"""

import argparse
import json
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from compare_judge import tau_b  # noqa: E402

FEATURES = {
    "fit": lambda p: p["fit"] / 100,
    "resp": lambda p: p["h"]["responsibilities"],
    "dom": lambda p: p["h"]["domain_mismatch"],
    "gap": lambda p: p["h"].get("primary_gap", 0),
    "loc": lambda p: p["h"].get("location_mismatch", 0),
    "months": lambda p: min(p["h"].get("gap_months") or 0, 24) / 12,
    "fit*resp": lambda p: p["fit"] / 100 * p["h"]["responsibilities"],
}

V3 = {"1": 44.2, "fit": 31.7, "resp": 46.2, "dom": -17.5}


def load(reports):
    acc = {}
    for path in reports:
        rep = json.loads(Path(path).read_text())
        d = Path(rep.get("dir") or "testdata/reference")
        pairs = {p["id"]: p for p in json.loads((d / "pairs.json").read_text())["pairs"]}
        for s in rep["pairs"]:
            if s.get("error") or s.get("reference") is None or s.get("fit") is None or not s.get("holistic"):
                continue
            a = acc.setdefault(s["id"], {"id": s["id"], "ref": s["reference"], "resume": pairs[s["id"]]["resume"],
                                         "set": d.name, "fits": [], "hs": []})
            a["fits"].append(s["fit"])
            a["hs"].append(s["holistic"])
    out = []
    for a in acc.values():
        keys = [k for k in a["hs"][0] if isinstance(a["hs"][0][k], (int, float)) or a["hs"][0][k] is None]
        h = {k: statistics.mean([x.get(k) or 0 for x in a["hs"]]) for k in keys}
        out.append({**a, "fit": statistics.mean(a["fits"]), "h": h})
    return out


def feature(p, f):
    if f in FEATURES:
        return FEATURES[f](p)
    if "*" in f:
        a, b = f.split("*", 1)
        return feature(p, a) * feature(p, b)
    return p["h"][f]


def row(p, feats):
    return [1.0] + [feature(p, f) for f in feats]


BLOCKER = "blocker"


def blk(p):
    return p["h"].get(BLOCKER, 0)


def load_probe(path, pairs):
    """Replace each pair's Holistic answers with the probe's averages."""
    probe = json.loads(Path(path).read_text())
    out = []
    for p in pairs:
        runs = [r for r in probe["pairs"].get(p["id"], []) if "answers" in r]
        if not runs:
            continue
        h = {}
        for q, spec in probe["questions"].items():
            vals = []
            for r in runs:
                a = r["answers"][q]
                vals.append(a["noul"] if spec["type"] == "noul" else a["score"] / (len(spec["criteria"]) - 1))
            h[q] = statistics.mean(vals)
        out.append({**p, "h": h})
    return out


def solve(A, b):
    n = len(A)
    M = [A[i][:] + [b[i]] for i in range(n)]
    for c in range(n):
        piv = max(range(c, n), key=lambda r: abs(M[r][c]))
        M[c], M[piv] = M[piv], M[c]
        if abs(M[c][c]) < 1e-12:
            raise ValueError("singular")
        for r in range(n):
            if r != c:
                f = M[r][c] / M[c][c]
                M[r] = [x - f * y for x, y in zip(M[r], M[c])]
    return [M[i][n] / M[i][i] for i in range(n)]


def fit(pairs, feats, ridge=1e-3):
    X = [[x * (1 - blk(p)) for x in row(p, feats)] for p in pairs]
    y = [p["ref"] for p in pairs]
    k = len(X[0])
    A = [[sum(r[i] * r[j] for r in X) + (ridge if i == j and i else 0) for j in range(k)] for i in range(k)]
    b = [sum(r[i] * t for r, t in zip(X, y)) for i in range(k)]
    return solve(A, b)


def predict(p, feats, w):
    x = sum(a * b for a, b in zip(row(p, feats), w)) * (1 - blk(p))
    return round(min(100, max(0, x)))


def report(name, pairs, feats, w=None):
    fixed = w is not None
    ws = w or fit(pairs, feats)
    ins = [predict(p, feats, ws) for p in pairs]
    if fixed:
        lpo = lro = ins
    else:
        lpo = [predict(p, feats, fit([q for q in pairs if q is not p], feats)) for p in pairs]
        by_resume = {}
        for p in pairs:
            by_resume.setdefault(p["resume"], []).append(p)
        models = {r: fit([q for q in pairs if q["resume"] != r], feats) for r in by_resume}
        lro = [predict(p, feats, models[p["resume"]]) for p in pairs]
    refs = [p["ref"] for p in pairs]
    mae = lambda pred: statistics.mean(abs(a - b) for a, b in zip(pred, refs))  # noqa: E731
    groups = {}
    for p, e in zip(pairs, lro):
        groups.setdefault(Path(p["resume"]).stem, []).append(abs(e - p["ref"]))
    coef = " ".join(f"{n}={c:.1f}" for n, c in zip(["1"] + feats, ws))
    print(f"{name:60s}\n{'':28s} in {mae(ins):5.1f}  pair-out {mae(lpo):5.1f}  resume-out {mae(lro):5.1f}  "
          f"tau-b {tau_b(lro, refs):.2f}  bias {statistics.mean(a - b for a, b in zip(lro, refs)):+.1f}")
    print(f"{'':28s} {coef}")
    print(f"{'':28s} " + "  ".join(f"{g} {statistics.mean(v):.1f}" for g, v in sorted(groups.items())))


MODELS = [
    ("blocker", ["fit", "resp", "dom"]),
    ("blocker", ["fit", "resp", "dom", "gap"]),
    ("blocker", ["fit", "resp", "dom", "fit*resp"]),
]

# Default models for a probe of the production questions (tuning.yaml);
# probes from the redesign round name the blocker blocker_v3 and need
# -models.
PROBE_MODELS = [
    ("blocker", ["role_match", "experience_short"]),
    ("blocker", ["fit", "role_match", "experience_short"]),
]


def main():
    global BLOCKER
    ap = argparse.ArgumentParser()
    ap.add_argument("-probe")
    ap.add_argument("-models", help='models as "blocker_key:feat,feat;...", replacing the built-in list')
    ap.add_argument("reports", nargs="+")
    a = ap.parse_args()
    pairs = load(a.reports)
    models = MODELS
    if a.probe:
        pairs = load_probe(a.probe, pairs)
        models = PROBE_MODELS
        for p in pairs:
            p["h"]["resp"] = p["h"]["responsibilities"]
            p["h"]["dom"] = p["h"]["domain_mismatch"]
    if a.models:
        models = [(m.split(":")[0], m.split(":")[1].split(",")) for m in a.models.split(";")]
    print(f"{len(pairs)} pairs, {len({p['resume'] for p in pairs})} resumes\n")
    if not a.probe:
        report("v3 (fixed)", pairs, ["fit", "resp", "dom"], [V3[k] for k in ["1", "fit", "resp", "dom"]])
    for BLOCKER, feats in models:
        report(f"{BLOCKER}: " + "+".join(feats), pairs, feats)


if __name__ == "__main__":
    main()
