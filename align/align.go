// Package align provides a generic, deterministic sequence-alignment core:
// find the longest order-preserving correspondence between two sequences
// via shared keys (the patience-diff technique - longest increasing
// subsequence over candidate matches, the same idea git/diff use), then
// fill the gaps between matches with a content-free, count-based rule.
// No randomness, no external state - identical inputs always produce
// identical output (invariant #9). Built for T4b's verse aligner; intended
// to be reused by the deferred T22 word aligner (Swete<->OSS).
package align

import "sort"

// Anchor is one confirmed correspondence between position AIdx in sequence
// A and position BIdx in sequence B, found by Anchors.
type Anchor struct {
	AIdx, BIdx int
}

// Anchors finds the longest order-preserving correspondence between a and
// b: positions where key(a[i]) == key(b[j]) for some comparable key K,
// chosen so both the A-indices and B-indices of the returned anchors are
// strictly increasing (each position is used in at most one anchor). This
// is patience-diff's "longest increasing subsequence over unique common
// elements" technique, generalized to a caller-chosen key instead of raw
// equality, so callers can normalize (eg ignore accents) without forcing
// O(n*m) comparison.
//
// Assumes each key is unique within a and within b respectively (true for
// verse (chapter,verse) tuples within one book+versification, the only
// caller today). With duplicate keys on either side, the result is still
// order-preserving and deterministic, but which duplicate matches which is
// unspecified.
func Anchors[T any, K comparable](a, b []T, key func(T) K) []Anchor {
	bIndexByKey := make(map[K]int, len(b))
	for j, v := range b {
		k := key(v)
		if _, exists := bIndexByKey[k]; !exists {
			bIndexByKey[k] = j
		}
	}

	type candidate struct{ aIdx, bIdx int }
	var candidates []candidate
	for i, v := range a {
		if j, ok := bIndexByKey[key(v)]; ok {
			candidates = append(candidates, candidate{i, j})
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	seq := make([]int, len(candidates))
	for i, c := range candidates {
		seq[i] = c.bIdx
	}
	lis := longestIncreasingSubsequence(seq)

	anchors := make([]Anchor, len(lis))
	for i, idx := range lis {
		anchors[i] = Anchor{candidates[idx].aIdx, candidates[idx].bIdx}
	}
	return anchors
}

// longestIncreasingSubsequence returns the indices (into seq) of a longest
// strictly increasing subsequence, via patience sorting - O(n log n).
func longestIncreasingSubsequence(seq []int) []int {
	if len(seq) == 0 {
		return nil
	}
	piles := make([]int, 0, len(seq))
	prev := make([]int, len(seq))
	for i, v := range seq {
		lo := sort.Search(len(piles), func(k int) bool { return seq[piles[k]] >= v })
		if lo > 0 {
			prev[i] = piles[lo-1]
		} else {
			prev[i] = -1
		}
		if lo == len(piles) {
			piles = append(piles, i)
		} else {
			piles[lo] = i
		}
	}
	result := make([]int, len(piles))
	k := piles[len(piles)-1]
	for idx := len(piles) - 1; idx >= 0; idx-- {
		result[idx] = k
		k = prev[k]
	}
	return result
}

// Group is one alignment correspondence produced by FillGap: a contiguous
// run of A indices and a contiguous run of B indices that correspond to
// each other. Exactly one side is empty for a pure insertion (A empty) or
// deletion (B empty).
type Group struct {
	AIdx []int
	BIdx []int
}

// FillGap turns a contiguous run of A indices and B indices with no
// internal anchor into Groups:
//   - both empty: no groups.
//   - one side empty: every index on the non-empty side becomes its own
//     Group with the other side empty (pure insertion/deletion - eg LXX-only
//     or canonical-only content).
//   - equal nonzero counts: paired 1:1 in order.
//   - unequal nonzero counts: the larger side's indices are distributed
//     across the smaller side's as evenly as possible (group sizes differ
//     by at most 1; any remainder goes to the earliest groups), each
//     smaller-side index getting its own Group containing its matched run
//     of the larger side.
//
// This is the mechanical, content-free fallback for a region with no
// label-based anchor: a deterministic function of the two counts, not a
// guess at which specific items correspond.
func FillGap(aIdx, bIdx []int) []Group {
	switch {
	case len(aIdx) == 0 && len(bIdx) == 0:
		return nil
	case len(aIdx) == 0:
		groups := make([]Group, len(bIdx))
		for i, b := range bIdx {
			groups[i] = Group{BIdx: []int{b}}
		}
		return groups
	case len(bIdx) == 0:
		groups := make([]Group, len(aIdx))
		for i, a := range aIdx {
			groups[i] = Group{AIdx: []int{a}}
		}
		return groups
	case len(aIdx) == len(bIdx):
		groups := make([]Group, len(aIdx))
		for i := range aIdx {
			groups[i] = Group{AIdx: []int{aIdx[i]}, BIdx: []int{bIdx[i]}}
		}
		return groups
	case len(aIdx) > len(bIdx):
		chunks := Distribute(len(aIdx), len(bIdx))
		groups := make([]Group, len(bIdx))
		pos := 0
		for i, n := range chunks {
			groups[i] = Group{AIdx: append([]int(nil), aIdx[pos:pos+n]...), BIdx: []int{bIdx[i]}}
			pos += n
		}
		return groups
	default:
		chunks := Distribute(len(bIdx), len(aIdx))
		groups := make([]Group, len(aIdx))
		pos := 0
		for i, n := range chunks {
			groups[i] = Group{AIdx: []int{aIdx[i]}, BIdx: append([]int(nil), bIdx[pos:pos+n]...)}
			pos += n
		}
		return groups
	}
}

// Distribute splits total items into n groups as evenly as possible (sizes
// differ by at most 1), with any remainder added to the earliest groups.
func Distribute(total, n int) []int {
	base, rem := total/n, total%n
	sizes := make([]int, n)
	for i := range sizes {
		sizes[i] = base
		if i < rem {
			sizes[i]++
		}
	}
	return sizes
}

// OpKind classifies one operation produced by AlignWeighted.
type OpKind int

const (
	OpSubstitute OpKind = iota // 1 A item : 1 B item, regardless of weight match
	OpDelete                   // A-only (no B counterpart)
	OpInsert                   // B-only (no A counterpart)
	OpMerge                    // 2 A items : 1 B item
	OpDivide                   // 1 A item : 2 B items
)

// Op is one chapter/group-level alignment operation produced by
// AlignWeighted, naming which A and B indices it consumed.
type Op struct {
	Kind OpKind
	AIdx []int
	BIdx []int
}

// AlignWeighted finds the minimum-cost alignment between two sequences of
// item weights (eg a chapter's verse count), via a generalized edit-
// distance / dynamic-time-warping DP: substitution cost is the absolute
// weight difference (0 for an exact size match - the strongest possible
// signal that two items correspond); an unmatched item costs its own
// weight (insertion/deletion); a 2:1 merge or 1:2 divide costs the
// absolute difference between the single item's weight and the SUM of the
// other side's two weights.
//
// This exists because raw label/position equality is unsafe once items
// have been renumbered or restructured (eg a book's chapter 3 splitting so
// part of it becomes the edition's new chapter 4): an edition's resulting
// chapter "3" can coincidentally share a number with an unrelated
// canonical chapter 3, producing a confidently wrong match. Verse COUNT
// is a far more specific signal - two unrelated chapters rarely have the
// exact same length - and a real merge/divide event is recognizable as the
// near-zero-cost way to reconcile a size mismatch (eg combining two source
// chapters whose sizes sum to within 1 of the target's size, the off-by-
// one being a real difference like a counted title verse) versus a much
// higher cost if the same items were forced into independent 1:1 or
// insert/delete operations instead.
//
// O(n*m) time and space; n,m are chapter counts (low hundreds at most for
// any book in this corpus), so this is fast in practice. Deterministic:
// ties are broken in a fixed preference order (substitute, delete, insert,
// merge, divide - the simplest explanation wins), so identical inputs
// always produce identical output (invariant #9).
func AlignWeighted(aWeights, bWeights []int) []Op {
	n, m := len(aWeights), len(bWeights)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	// opAt[i][j] records which operation reached dp[i][j], for backtracking.
	opAt := make([][]OpKind, n+1)
	for i := range opAt {
		opAt[i] = make([]OpKind, m+1)
	}

	for i := 1; i <= n; i++ {
		dp[i][0] = dp[i-1][0] + aWeights[i-1]
		opAt[i][0] = OpDelete
	}
	for j := 1; j <= m; j++ {
		dp[0][j] = dp[0][j-1] + bWeights[j-1]
		opAt[0][j] = OpInsert
	}

	const inf = int(^uint(0) >> 1)
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			best := dp[i-1][j-1] + absInt(aWeights[i-1]-bWeights[j-1])
			bestOp := OpSubstitute

			if c := dp[i-1][j] + aWeights[i-1]; c < best {
				best, bestOp = c, OpDelete
			}
			if c := dp[i][j-1] + bWeights[j-1]; c < best {
				best, bestOp = c, OpInsert
			}
			mergeCost := inf
			if i >= 2 {
				mergeCost = dp[i-2][j-1] + absInt(aWeights[i-2]+aWeights[i-1]-bWeights[j-1])
				if mergeCost < best {
					best, bestOp = mergeCost, OpMerge
				}
			}
			if j >= 2 {
				if c := dp[i-1][j-2] + absInt(aWeights[i-1]-(bWeights[j-2]+bWeights[j-1])); c < best {
					best, bestOp = c, OpDivide
				}
			}

			dp[i][j] = best
			opAt[i][j] = bestOp
		}
	}

	var ops []Op
	i, j := n, m
	for i > 0 || j > 0 {
		switch opAt[i][j] {
		case OpDelete:
			ops = append(ops, Op{Kind: OpDelete, AIdx: []int{i - 1}})
			i--
		case OpInsert:
			ops = append(ops, Op{Kind: OpInsert, BIdx: []int{j - 1}})
			j--
		case OpMerge:
			ops = append(ops, Op{Kind: OpMerge, AIdx: []int{i - 2, i - 1}, BIdx: []int{j - 1}})
			i -= 2
			j--
		case OpDivide:
			ops = append(ops, Op{Kind: OpDivide, AIdx: []int{i - 1}, BIdx: []int{j - 2, j - 1}})
			i--
			j -= 2
		default: // OpSubstitute
			ops = append(ops, Op{Kind: OpSubstitute, AIdx: []int{i - 1}, BIdx: []int{j - 1}})
			i--
			j--
		}
	}
	// Reverse into forward order.
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	return ops
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ProportionalAllocate splits total items across len(weights) buckets in
// proportion to each weight, summing to exactly total. Uses the largest-
// remainder method (the standard, well-defined apportionment algorithm):
// each bucket gets floor(total*weight/sumWeights), then the remainder is
// handed out one at a time to the buckets with the largest fractional
// remainder, ties broken by earliest index - fully deterministic.
func ProportionalAllocate(total int, weights []int) []int {
	sumW := 0
	for _, w := range weights {
		sumW += w
	}
	if sumW == 0 {
		return make([]int, len(weights))
	}

	raw := make([]float64, len(weights))
	alloc := make([]int, len(weights))
	sumFloor := 0
	for i, w := range weights {
		raw[i] = float64(total) * float64(w) / float64(sumW)
		alloc[i] = int(raw[i])
		sumFloor += alloc[i]
	}

	type frac struct {
		idx int
		f   float64
	}
	fracs := make([]frac, len(weights))
	for i := range weights {
		fracs[i] = frac{i, raw[i] - float64(alloc[i])}
	}
	sort.Slice(fracs, func(i, j int) bool {
		if fracs[i].f != fracs[j].f {
			return fracs[i].f > fracs[j].f
		}
		return fracs[i].idx < fracs[j].idx
	})

	remainder := total - sumFloor
	for i := 0; i < remainder; i++ {
		alloc[fracs[i].idx]++
	}
	return alloc
}
