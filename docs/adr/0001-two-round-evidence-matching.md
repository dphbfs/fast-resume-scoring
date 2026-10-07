# Two Jev rounds for evidence matching

The Resume Checker links Evidence Units to Requirements in two rounds: a
Retrieval Round (per Evidence Unit, one Choice over all Requirements plus
`none`, keep the top 5 above p >= 0.02) and then a Strength Round (one
`strong | partial | weak | none` Choice per retrieved pair). We considered
asking the Strength question for every Evidence Unit × Requirement pair,
which is simpler and can't miss a pair at retrieval. We rejected it because
it multiplies Strength questions by the Requirement count (~40× instead of
<= 5× per Evidence Unit), and the cheap Retrieval Round lets each round be
evaluated on its own: retrieval recall and Evidence Link precision.

Consequence: a pair the Retrieval Round drops can never be linked. If eval
retrieval recall stays below 95%, switch retrieval to one Noul per
Requirement (independent probabilities) before dropping the round.

Update 2026-10-01: Noul retrieval was tried (`CHECKER_RETRIEVAL_MODE=noul`).
It lifts retrieval recall to 97%, but Coverage does not improve, because
the Strength Round then links too many borderline pairs. The two-round
design stands; the default stays single-Choice retrieval until the
Strength Round is more precise.

Update 2026-10-01 (later): with a gate Noul per pair in the Strength Round
deciding whether to link, higher-recall retrieval pays off. The default is
now `narrow` retrieval (Choice repeated over the best 16, then 8) with the
gate; still two rounds. Noul retrieval reaches the same Coverage at higher
cost and lower link precision.
