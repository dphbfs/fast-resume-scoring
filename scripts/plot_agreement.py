#!/usr/bin/env python3
"""Draw docs/img/agreement.svg: Match Score vs the generative reference on
the second sealed set, with the recruiter judge for context.

Usage: scripts/plot_agreement.py [e2e-report.json] [judge.json] [out.svg]

Defaults are the frozen run of Match Score v4 (docs/final-report-v1.md).
The SVG is static (README images cannot run scripts) and carries its own
light and dark styles.
"""

import json
import sys
from pathlib import Path

REPORT = "eval/reports/e2e/2026-10-06T01-32-00Z.json"
JUDGE = "testdata/final2/judge.json"
OUT = "docs/img/agreement.svg"

W, H = 640, 470
LEFT, RIGHT, TOP, BOTTOM = 64, 24, 92, 64
PW, PH = W - LEFT - RIGHT, H - TOP - BOTTOM


def x(v):
    return LEFT + v / 100 * PW


def y(v):
    return TOP + PH - v / 100 * PH


def mae(pairs):
    return sum(abs(a - b) for a, b in pairs) / len(pairs)


def main():
    report = sys.argv[1] if len(sys.argv) > 1 else REPORT
    judge_path = sys.argv[2] if len(sys.argv) > 2 else JUDGE
    out = Path(sys.argv[3] if len(sys.argv) > 3 else OUT)

    pairs = json.loads(Path(report).read_text())["pairs"]
    judge = {p["pair"]: p["score"] for p in json.loads(Path(judge_path).read_text())["pairs"]}
    jev = [(p["reference"], p["match"]) for p in pairs]
    jud = [(p["reference"], judge[p["id"]]) for p in pairs if p["id"] in judge]

    s = []
    s.append(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W} {H}" width="{W}" height="{H}" '
             'role="img" aria-labelledby="t d">')
    s.append('<title id="t">Match Score vs the generative reference, held-out set</title>')
    s.append(f'<desc id="d">Scatter of {len(jev)} held-out resume-job pairs. Jev Match Score MAE '
             f'{mae(jev):.1f} points against one Claude Opus 5 call per pair; an independent recruiter '
             f'judge scores MAE {mae(jud):.1f} against the same reference.</desc>')
    s.append("""<style>
  .bg { fill: #fcfcfb }
  .title { fill: #0b0b0b; font: 600 16px system-ui, -apple-system, "Segoe UI", sans-serif }
  .sub, .tick, .axis-label, .legend, .note { fill: #52514e; font: 12px system-ui, -apple-system, "Segoe UI", sans-serif }
  .tick { fill: #898781 }
  .grid { stroke: #e1e0d9; stroke-width: 1 }
  .base { stroke: #c3c2b7; stroke-width: 1 }
  .band { fill: #cde2fb; opacity: 0.45 }
  .diag { stroke: #898781; stroke-width: 1.5; stroke-dasharray: 4 4 }
  .jev { fill: #2a78d6; stroke: #fcfcfb; stroke-width: 2 }
  .judge { fill: none; stroke: #898781; stroke-width: 1.5 }
  @media (prefers-color-scheme: dark) {
    .bg { fill: #1a1a19 }
    .title { fill: #ffffff }
    .sub, .axis-label, .legend, .note { fill: #c3c2b7 }
    .grid { stroke: #2c2c2a }
    .base { stroke: #383835 }
    .band { fill: #184f95; opacity: 0.35 }
    .jev { fill: #3987e5; stroke: #1a1a19 }
  }
</style>""")
    s.append(f'<rect class="bg" width="{W}" height="{H}" rx="8"/>')
    s.append(f'<text class="title" x="{LEFT}" y="30">Held-out agreement with the generative reference</text>')
    s.append(f'<text class="sub" x="{LEFT}" y="50">{len(jev)} unseen resume–job pairs, '
             '4 resumes. On the dashed line = same score as the reference.</text>')

    # Legend: shape carries identity as well as color.
    ly = 74
    s.append(f'<circle class="jev" cx="{LEFT + 6}" cy="{ly - 4}" r="5"/>')
    s.append(f'<text class="legend" x="{LEFT + 16}" y="{ly}">Jev Match Score (MAE {mae(jev):.1f})</text>')
    lx = LEFT + 236
    s.append(f'<circle class="judge" cx="{lx + 6}" cy="{ly - 4}" r="4.5"/>')
    s.append(f'<text class="legend" x="{lx + 16}" y="{ly}">Recruiter judge, other model family '
             f'(MAE {mae(jud):.1f})</text>')

    # ±10 band around y = x, clipped to the plot.
    band = [(0, 10), (90, 100), (100, 100), (100, 90), (10, 0), (0, 0)]
    s.append('<polygon class="band" points="' + " ".join(f"{x(a):.1f},{y(b):.1f}" for a, b in band) + '"/>')
    s.append(f'<text class="note" x="{x(13):.1f}" y="{y(8):.1f}" transform="rotate(-{45 * PH / PW:.0f} '
             f'{x(13):.1f} {y(8):.1f})" font-size="11">within ±10</text>')

    for v in range(0, 101, 20):
        s.append(f'<line class="grid" x1="{x(0)}" x2="{x(100)}" y1="{y(v):.1f}" y2="{y(v):.1f}"/>')
        s.append(f'<line class="grid" x1="{x(v):.1f}" x2="{x(v):.1f}" y1="{y(0)}" y2="{y(100)}"/>')
        s.append(f'<text class="tick" x="{x(v):.1f}" y="{y(0) + 18:.1f}" text-anchor="middle">{v}</text>')
        s.append(f'<text class="tick" x="{x(0) - 8:.1f}" y="{y(v) + 4:.1f}" text-anchor="end">{v}</text>')
    s.append(f'<line class="base" x1="{x(0)}" x2="{x(100)}" y1="{y(0)}" y2="{y(0)}"/>')
    s.append(f'<line class="base" x1="{x(0)}" x2="{x(0)}" y1="{y(0)}" y2="{y(100)}"/>')
    s.append(f'<line class="diag" x1="{x(0)}" y1="{y(0)}" x2="{x(100)}" y2="{y(100)}"/>')

    s.append(f'<text class="axis-label" x="{x(50)}" y="{H - 18}" text-anchor="middle">'
             'Reference score (Claude Opus 5, one prompt per pair)</text>')
    s.append(f'<text class="axis-label" transform="translate(18 {y(50)}) rotate(-90)" '
             'text-anchor="middle">Predicted score</text>')

    # Context first, so the Jev marks sit on top.
    for ref, score in jud:
        s.append(f'<circle class="judge" cx="{x(ref):.1f}" cy="{y(score):.1f}" r="4.5"/>')
    for ref, score in jev:
        s.append(f'<circle class="jev" cx="{x(ref):.1f}" cy="{y(score):.1f}" r="5"/>')

    s.append("</svg>")
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text("\n".join(s) + "\n")
    print(f"wrote {out}: {len(jev)} pairs, Jev MAE {mae(jev):.1f}, judge MAE {mae(jud):.1f}")


if __name__ == "__main__":
    main()
