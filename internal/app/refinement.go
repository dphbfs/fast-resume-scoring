package app

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
)

// refinementState is background shared by every Refinement question.
type refinementState struct {
	JobTitle   string `json:"job_title"`
	JobSummary string `json:"job_summary"`
}

// refinementRound asks, for every validated Requirement (unique by
// lowercase value), whether it is Filler, which other Requirement it
// duplicates, which one it is an alternative to, and how important it is.
// It then drops Filler, rewrites duplicates to their canonical value, builds
// Alternative Groups, and records Importance.
func (e *Extractor) refinementRound(ctx context.Context, r *run) error {
	titleDrops := e.dropJobTitle(ctx, r)
	values, mentions := uniqueRequirements(r)
	if len(values) == 0 {
		return nil
	}

	questions := map[string]port.Question{}
	for i, v := range values {
		ms := mentions[i]
		questions[fmt.Sprintf("filler_%d", i)] = e.prompts.fillerQuestion(v, ms)
		if !e.cfg.SkipImportance {
			questions[fmt.Sprintf("importance_%d", i)] = e.prompts.importanceQuestion(v, ms)
		}
		if opts := duplicateOptions(values, i, maxAllDuplicateOptions); len(opts) > 0 {
			questions[fmt.Sprintf("dup_%d", i)] = e.prompts.duplicateQuestion(v, ms, opts)
		}
		if opts := sameSentenceValues(r, values, i); len(opts) > 0 {
			questions[fmt.Sprintf("alt_%d", i)] = e.prompts.alternativeQuestion(v, ms, opts)
		}
	}

	answers, err := e.classifyBatched(ctx, refinementState{JobTitle: r.jd.Title, JobSummary: r.summary}, questions)
	if err != nil {
		return err
	}

	// 1. Filler.
	trs := make([]domain.TraceRequirement, len(values))
	keep := make([]bool, len(values))
	for i := range values {
		a := answers[fmt.Sprintf("filler_%d", i)]
		trs[i] = domain.TraceRequirement{Value: values[i], Mentions: len(mentions[i]), KeepP: a.Probabilities[fillerKeep]}
		if a.Probabilities[fillerKeep] >= minKeepMass {
			keep[i] = true
			trs[i].Kept = true
			continue
		}
		trs[i].FillerKind = topOption(a.Probabilities, e.prompts.fillerReasons)
		e.metrics.Add("refinement.dropped."+trs[i].FillerKind, 1)
		e.log.DebugContext(ctx, "filler dropped", "requirement", values[i])
	}

	// 2. Duplicates: merge two kept Requirements only when each names the
	// other as a duplicate. One-way links were mostly related-but-different
	// pairs in eval ("dashboards" -> "Grafana dashboards").
	index := map[string]int{}
	for i, v := range values {
		index[v] = i
	}
	dupOf := make([]int, len(values))
	for i := range values {
		dupOf[i] = -1
		if a, ok := answers[fmt.Sprintf("dup_%d", i)]; ok && keep[i] {
			if j, ok := linkedValue(a, e.prompts.duplicateReasons, index, minMergeMass); ok && keep[j] {
				dupOf[i] = j
				trs[i].DuplicateOf = values[j]
			}
		}
	}
	dups := newUnionFind(len(values))
	for i, j := range dupOf {
		if j > i && dupOf[j] == i {
			dups.union(i, j)
			r.trace.Merges = append(r.trace.Merges, [2]string{values[i], values[j]})
			e.metrics.Add("refinement.merged", 1)
			e.log.DebugContext(ctx, "duplicate merged", "requirement", values[i], "and", values[j])
		} else if j >= 0 && dupOf[j] != i {
			e.metrics.Add("refinement.one_way_duplicate", 1)
		}
	}
	canonical := make([]int, len(values)) // representative: most mentions, then first
	for _, members := range dups.sets() {
		best := slices.MinFunc(members, func(a, b int) int {
			return cmp.Or(cmp.Compare(len(mentions[b]), len(mentions[a])), cmp.Compare(a, b))
		})
		for _, m := range members {
			canonical[m] = best
			if m != best {
				trs[m].MergedInto = values[best]
			}
		}
	}

	// 3. Importance: Score / top level; a merged Requirement keeps the max.
	// r.importance also records which canonical values were kept, so a
	// skipped Importance is stored as 0.
	top := float64(len(e.prompts.importanceLevels) - 1)
	r.importance = map[string]float64{}
	for i := range values {
		if !keep[i] {
			continue
		}
		k := strings.ToLower(values[canonical[i]])
		if e.cfg.SkipImportance {
			r.importance[k] = 0
			continue
		}
		a := answers[fmt.Sprintf("importance_%d", i)]
		if a.Score == nil {
			return fmt.Errorf("requirement %q: importance answer has no score", values[i])
		}
		r.importance[k] = max(r.importance[k], *a.Score/top)
		trs[i].Score, trs[i].Importance = *a.Score, *a.Score/top
	}

	// 4. Alternatives between kept canonical Requirements.
	alts := newUnionFind(len(values))
	for i := range values {
		a, ok := answers[fmt.Sprintf("alt_%d", i)]
		if !ok || !keep[i] {
			continue
		}
		if j, ok := linkedValue(a, e.prompts.alternativeReasons, index, minAlternativeMass); ok && keep[j] &&
			canonical[i] != canonical[j] {
			alts.union(canonical[i], canonical[j])
			trs[i].AlternativeOf = values[j]
			e.metrics.Add("refinement.alternative_links", 1)
		}
	}
	r.groups = nil
	for _, members := range alts.sets() {
		if len(members) < 2 {
			continue
		}
		slices.Sort(members)
		g := make([]string, len(members))
		for k, m := range members {
			g[k] = values[m]
		}
		r.groups = append(r.groups, g)
	}
	slices.SortFunc(r.groups, func(a, b []string) int { return cmp.Compare(index[a[0]], index[b[0]]) })
	r.trace.Refinement = append(trs, titleDrops...)
	r.trace.Groups = r.groups

	// Apply Filler drops and duplicate rewrites to the accepted Candidates.
	kept := r.accepted[:0]
	for _, a := range r.accepted {
		i := index[a.Text]
		if !keep[i] {
			continue
		}
		a.Text = values[canonical[i]]
		kept = append(kept, a)
	}
	r.accepted = kept
	e.metrics.Add("refinement.kept", int64(len(r.importance)))
	return nil
}

// roleWords mark a phrase as a job title rather than a skill.
var roleWords = map[string]bool{
	"engineer": true, "engineers": true, "developer": true, "developers": true,
	"architect": true, "manager": true, "lead": true, "scientist": true,
	"analyst": true, "administrator": true, "designer": true, "sre": true,
}

// titleWords lowercases s and keeps its words, dropping punctuation.
func titleWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#' || r == '.')
	})
}

// isJobTitleFragment reports whether value is the job title, or a run of two
// or more of its words that includes a role word ("Backend Engineer" in
// "Senior Backend Engineer"). Single words are kept: in "Senior Software
// Engineer, Android", "Android" is a real skill.
func isJobTitleFragment(value, title string) bool {
	v, t := titleWords(value), titleWords(title)
	if len(v) == 0 || len(t) == 0 {
		return false
	}
	if slices.Equal(v, t) {
		return true
	}
	if len(v) < 2 || !slices.ContainsFunc(v, func(w string) bool { return roleWords[w] }) {
		return false
	}
	for i := 0; i+len(v) <= len(t); i++ {
		if slices.Equal(t[i:i+len(v)], v) {
			return true
		}
	}
	return false
}

// dropJobTitle removes validated Candidates that are job title fragments
// before any Refinement question is asked, and returns trace entries for
// them.
func (e *Extractor) dropJobTitle(ctx context.Context, r *run) []domain.TraceRequirement {
	var drops []domain.TraceRequirement
	seen := map[string]bool{}
	kept := r.accepted[:0]
	for _, a := range r.accepted {
		if !isJobTitleFragment(a.Text, r.jd.Title) {
			kept = append(kept, a)
			continue
		}
		if !seen[strings.ToLower(a.Text)] {
			seen[strings.ToLower(a.Text)] = true
			drops = append(drops, domain.TraceRequirement{Value: a.Text, FillerKind: "job_title"})
			e.metrics.Add("refinement.dropped.job_title", 1)
			e.log.DebugContext(ctx, "job title dropped", "requirement", a.Text)
		}
	}
	r.accepted = kept
	return drops
}

// uniqueRequirements returns validated values unique by lowercase (first
// spelling wins), with every mention of each.
func uniqueRequirements(r *run) ([]string, [][]mention) {
	sentences := make(map[domain.Ref]domain.ContextSentence, len(r.sentences))
	for _, s := range r.sentences {
		sentences[s.Ref] = s
	}
	var values []string
	var mentions [][]mention
	seen := map[string]int{}
	refs := map[int][]domain.Ref{}
	for k, a := range r.accepted {
		key := strings.ToLower(a.Text)
		i, ok := seen[key]
		if !ok {
			i = len(values)
			seen[key] = i
			values = append(values, a.Text)
			mentions = append(mentions, nil)
		}
		r.accepted[k].Text = values[i]
		if !slices.Contains(refs[i], a.Ref) {
			refs[i] = append(refs[i], a.Ref)
			s := sentences[a.Ref]
			mentions[i] = append(mentions[i], mention{Section: s.Section, Sentence: s.Text})
		}
	}
	return values, mentions
}

// duplicateOptions offers every other value when the list is short, else
// only values that share a word or look alike (character trigrams).
func duplicateOptions(values []string, i, maxAll int) []string {
	var out []string
	for j, v := range values {
		if j == i {
			continue
		}
		if len(values) <= maxAll || similar(values[i], v) {
			out = append(out, v)
		}
	}
	return out
}

func similar(a, b string) bool {
	wa, wb := strings.Fields(strings.ToLower(a)), strings.Fields(strings.ToLower(b))
	for _, w := range wa {
		if !isStopword(w) && !genericWords[w] && slices.Contains(wb, w) {
			return true
		}
	}
	return trigramJaccard(strings.ToLower(a), strings.ToLower(b)) >= 0.35
}

func trigramJaccard(a, b string) float64 {
	grams := func(s string) map[string]bool {
		s = " " + s + " "
		out := map[string]bool{}
		r := []rune(s)
		for i := 0; i+3 <= len(r); i++ {
			out[string(r[i:i+3])] = true
		}
		return out
	}
	ga, gb := grams(a), grams(b)
	inter := 0
	for g := range ga {
		if gb[g] {
			inter++
		}
	}
	union := len(ga) + len(gb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// sameSentenceValues are the other values sharing a sentence with values[i].
func sameSentenceValues(r *run, values []string, i int) []string {
	refs := map[domain.Ref]bool{}
	for _, a := range r.accepted {
		if a.Text == values[i] {
			refs[a.Ref] = true
		}
	}
	var out []string
	for _, a := range r.accepted {
		if refs[a.Ref] && a.Text != values[i] && !slices.Contains(out, a.Text) {
			out = append(out, a.Text)
		}
	}
	return out
}

// linkedValue returns the index of the Requirement option a Choice selected,
// if the Requirement options together hold at least minMass.
func linkedValue(a port.Answer, reasons map[string]string, index map[string]int, minMass float64) (int, bool) {
	var mass, bestP float64
	best := ""
	for opt, p := range a.Probabilities {
		if _, isReason := reasons[opt]; isReason {
			continue
		}
		if _, isValue := index[opt]; !isValue {
			continue
		}
		mass += p
		if p > bestP || (p == bestP && opt < best) {
			best, bestP = opt, p
		}
	}
	if best == "" || mass < minMass {
		return 0, false
	}
	return index[best], true
}

// topOption returns the most probable of the given options.
func topOption(probs map[string]float64, options map[string]string) string {
	best, bestP := "", -1.0
	for _, k := range slices.Sorted(maps.Keys(options)) {
		if probs[k] > bestP {
			best, bestP = k, probs[k]
		}
	}
	return best
}

// classifyBatched sends questions in requests whose question JSON stays
// under e.refinementBatchChars, concurrently, and merges the answers.
func (e *Extractor) classifyBatched(ctx context.Context, state any, questions map[string]port.Question) (map[string]port.Answer, error) {
	ids := slices.Sorted(maps.Keys(questions))
	var batches [][]string
	size := 0
	for _, id := range ids {
		raw, err := json.Marshal(questions[id])
		if err != nil {
			return nil, err
		}
		if len(batches) == 0 || size+len(raw) > e.refinementBatchChars {
			batches = append(batches, nil)
			size = 0
		}
		batches[len(batches)-1] = append(batches[len(batches)-1], id)
		size += len(raw)
	}

	results := make([]map[string]port.Answer, len(batches))
	g, gctx := errgroup.WithContext(ctx)
	for b, batch := range batches {
		g.Go(func() error {
			qs := make(map[string]port.Question, len(batch))
			for _, id := range batch {
				qs[id] = questions[id]
			}
			resp, err := e.classifier.Classify(gctx, port.ClassifyRequest{State: state, Questions: qs})
			if err != nil {
				return err
			}
			e.addUsage("extract.refinement", resp.Usage)
			results[b] = resp.Answers
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	e.metrics.Add("refinement.requests", int64(len(batches)))

	answers := make(map[string]port.Answer, len(questions))
	for _, res := range results {
		maps.Copy(answers, res)
	}
	return answers, nil
}

// unionFind groups indexes into disjoint sets.
type unionFind []int

func newUnionFind(n int) unionFind {
	u := make(unionFind, n)
	for i := range u {
		u[i] = i
	}
	return u
}

func (u unionFind) find(i int) int {
	for u[i] != i {
		u[i] = u[u[i]]
		i = u[i]
	}
	return i
}

func (u unionFind) union(a, b int) { u[u.find(a)] = u.find(b) }

// sets returns root -> members for every set.
func (u unionFind) sets() map[int][]int {
	out := map[int][]int{}
	for i := range u {
		r := u.find(i)
		out[r] = append(out[r], i)
	}
	return out
}
