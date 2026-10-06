package domain

import "math"

// Fit is how well a Resume covers a Job Description (docs/adr/0002).
// Score is null when there is nothing to score; ByTier holds only Tiers
// with scored items; Gaps lists required items with Coverage none, by ID.
type Fit struct {
	Score  *int         `json:"score"`
	ByTier map[Tier]int `json:"by_tier"`
	Gaps   []string     `json:"gaps"`
}

// FitWeights are the Fit Score's Coverage credits and Tier weights
// (docs/adr/0002), read from the tuning file (tuning/tuning.yaml). A
// strength or Tier without an entry counts 0.
type FitWeights struct {
	Credit     map[EvidenceStrength]float64
	TierWeight map[Tier]float64
}

// tierRank orders Tiers: mentioned (or unknown) 0, preferred 1, required 2.
func tierRank(t Tier) int {
	switch t {
	case TierRequired:
		return 2
	case TierPreferred:
		return 1
	}
	return 0
}

// ScoreFit computes the Fit Score: the Tier-weighted average credit of each
// scored item, where an Alternative Group is one item (its best member's
// Coverage, its strongest member's Tier) and every other Requirement is one
// item. Only the groups' Members are read; members missing from reqs are
// ignored, and a group with none left is dropped. A Requirement without a
// Tier counts as mentioned.
func ScoreFit(reqs []RequirementCoverage, groups []GroupCoverage, fw FitWeights) Fit {
	type item struct {
		id       string
		tier     Tier
		coverage EvidenceStrength
	}
	groupOf := map[string]int{}
	for gi, g := range groups {
		for _, id := range g.Members {
			if _, ok := groupOf[id]; !ok {
				groupOf[id] = gi
			}
		}
	}
	var items []item
	at := map[int]int{} // group index -> its item
	for _, r := range reqs {
		tier := r.Tier
		if tierRank(tier) == 0 {
			tier = TierMentioned
		}
		gi, grouped := groupOf[r.ID]
		if !grouped {
			items = append(items, item{r.ID, tier, r.Coverage})
			continue
		}
		i, seen := at[gi]
		if !seen {
			at[gi] = len(items)
			items = append(items, item{groups[gi].ID, tier, r.Coverage})
			continue
		}
		if tierRank(tier) > tierRank(items[i].tier) {
			items[i].tier = tier
		}
		if r.Coverage.Rank() > items[i].coverage.Rank() {
			items[i].coverage = r.Coverage
		}
	}

	fit := Fit{ByTier: map[Tier]int{}, Gaps: []string{}}
	if len(items) == 0 {
		return fit
	}
	var earned, total float64
	tierEarned, tierTotal := map[Tier]float64{}, map[Tier]float64{}
	for _, it := range items {
		w, c := fw.TierWeight[it.tier], fw.Credit[it.coverage]
		earned += w * c
		total += w
		tierEarned[it.tier] += c
		tierTotal[it.tier]++
		if it.tier == TierRequired && it.coverage.Rank() == 0 {
			fit.Gaps = append(fit.Gaps, it.id)
		}
	}
	score := percent(earned, total)
	fit.Score = &score
	for t, n := range tierTotal {
		fit.ByTier[t] = percent(tierEarned[t], n)
	}
	return fit
}

func percent(part, whole float64) int {
	return int(math.Round(100 * part / whole))
}
