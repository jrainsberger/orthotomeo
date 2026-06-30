package align_test

import (
	"reflect"
	"testing"

	"github.com/jrainsberger/orthotomeo/align"
)

func TestAnchorsFindsOrderPreservingMatches(t *testing.T) {
	// a: [1,2,3,4,5]  b: [9,2,3,9,4,9,5]  - matches on 2,3,4,5; 1 is
	// canonical-only, the 9s are edition-only insertions.
	a := []int{1, 2, 3, 4, 5}
	b := []int{9, 2, 3, 9, 4, 9, 5}
	got := align.Anchors(a, b, func(v int) int { return v })

	want := []align.Anchor{{1, 1}, {2, 2}, {3, 4}, {4, 6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Anchors = %v, want %v", got, want)
	}
}

func TestAnchorsPicksLongestOrderPreservingChain(t *testing.T) {
	// b's matching positions for a=[1,2,3] appear out of order (3 before
	// 2) - only one of {1,2} or {1,3} can be a valid increasing chain, not
	// both 2 and 3, since b's index for "2" (3) is after b's index for "3"
	// (1). The longest valid chain is length 2: {1->0, 3->1} or {1->0,2->3}.
	a := []int{1, 2, 3}
	b := []int{1, 3, 9, 2}
	got := align.Anchors(a, b, func(v int) int { return v })

	for i := 1; i < len(got); i++ {
		if got[i].AIdx <= got[i-1].AIdx || got[i].BIdx <= got[i-1].BIdx {
			t.Fatalf("Anchors not order-preserving: %v", got)
		}
	}
	if len(got) != 2 {
		t.Errorf("len(Anchors) = %d, want 2 (longest valid chain)", len(got))
	}
}

func TestAnchorsEmptyOnNoOverlap(t *testing.T) {
	got := align.Anchors([]int{1, 2, 3}, []int{4, 5, 6}, func(v int) int { return v })
	if got != nil {
		t.Errorf("Anchors = %v, want nil", got)
	}
}

func TestFillGapEqualCounts(t *testing.T) {
	got := align.FillGap([]int{10, 11, 12}, []int{50, 51, 52})
	want := []align.Group{
		{AIdx: []int{10}, BIdx: []int{50}},
		{AIdx: []int{11}, BIdx: []int{51}},
		{AIdx: []int{12}, BIdx: []int{52}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FillGap = %+v, want %+v", got, want)
	}
}

func TestFillGapMerge(t *testing.T) {
	// 5 A items into 2 B items: sizes differ by at most 1, remainder
	// (5%2=1) goes to the earliest group.
	got := align.FillGap([]int{1, 2, 3, 4, 5}, []int{100, 101})
	want := []align.Group{
		{AIdx: []int{1, 2, 3}, BIdx: []int{100}},
		{AIdx: []int{4, 5}, BIdx: []int{101}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FillGap (merge) = %+v, want %+v", got, want)
	}
}

func TestFillGapDivide(t *testing.T) {
	got := align.FillGap([]int{1, 2}, []int{100, 101, 102, 103, 104})
	want := []align.Group{
		{AIdx: []int{1}, BIdx: []int{100, 101, 102}},
		{AIdx: []int{2}, BIdx: []int{103, 104}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FillGap (divide) = %+v, want %+v", got, want)
	}
}

func TestFillGapPureInsertAndDelete(t *testing.T) {
	ins := align.FillGap(nil, []int{1, 2})
	want := []align.Group{{BIdx: []int{1}}, {BIdx: []int{2}}}
	if !reflect.DeepEqual(ins, want) {
		t.Errorf("FillGap (insert) = %+v, want %+v", ins, want)
	}

	del := align.FillGap([]int{1, 2}, nil)
	wantDel := []align.Group{{AIdx: []int{1}}, {AIdx: []int{2}}}
	if !reflect.DeepEqual(del, wantDel) {
		t.Errorf("FillGap (delete) = %+v, want %+v", del, wantDel)
	}
}

func TestFillGapBothEmpty(t *testing.T) {
	if got := align.FillGap(nil, nil); got != nil {
		t.Errorf("FillGap(nil,nil) = %v, want nil", got)
	}
}

func TestDistributeSumsToTotal(t *testing.T) {
	for _, tc := range []struct{ total, n int }{{10, 3}, {7, 7}, {1, 5}, {100, 9}} {
		sizes := align.Distribute(tc.total, tc.n)
		if len(sizes) != tc.n {
			t.Errorf("Distribute(%d,%d): len = %d, want %d", tc.total, tc.n, len(sizes), tc.n)
		}
		sum := 0
		for _, s := range sizes {
			sum += s
		}
		if sum != tc.total {
			t.Errorf("Distribute(%d,%d): sum = %d, want %d", tc.total, tc.n, sum, tc.total)
		}
		max, min := sizes[0], sizes[0]
		for _, s := range sizes {
			if s > max {
				max = s
			}
			if s < min {
				min = s
			}
		}
		if max-min > 1 {
			t.Errorf("Distribute(%d,%d): sizes %v differ by more than 1", tc.total, tc.n, sizes)
		}
	}
}

func TestProportionalAllocateSumsToTotalAndIsProportional(t *testing.T) {
	// 39 total, weights [20,18] (Ps9+Ps10 vs the merged LXX chapter) should
	// allocate close to 20.5/17.5, ie [21,18] or [20,19] depending on
	// rounding - what matters is it sums exactly and roughly tracks weight.
	alloc := align.ProportionalAllocate(39, []int{20, 18})
	if len(alloc) != 2 {
		t.Fatalf("len = %d, want 2", len(alloc))
	}
	if alloc[0]+alloc[1] != 39 {
		t.Errorf("sum = %d, want 39", alloc[0]+alloc[1])
	}
	if alloc[0] < alloc[1] {
		t.Errorf("alloc = %v, want the larger weight (20) to get >= share of the smaller (18)", alloc)
	}
}

func TestProportionalAllocateDeterministicTieBreak(t *testing.T) {
	// 10 total split across 3 equal weights: 10/3 = 3.33 each, remainder 1
	// goes to the earliest index.
	got := align.ProportionalAllocate(10, []int{1, 1, 1})
	want := []int{4, 3, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ProportionalAllocate(10,[1,1,1]) = %v, want %v", got, want)
	}
}

// TestAlignWeightedPrefersSubstituteOnExactMatch mirrors the common,
// non-divergent case: every chapter's size matches exactly, so the cheapest
// (and only zero-cost) path is a straight 1:1 substitution throughout.
func TestAlignWeightedPrefersSubstituteOnExactMatch(t *testing.T) {
	ops := align.AlignWeighted([]int{20, 32, 21}, []int{20, 32, 21})
	if len(ops) != 3 {
		t.Fatalf("len(ops) = %d, want 3", len(ops))
	}
	for i, op := range ops {
		if op.Kind != align.OpSubstitute {
			t.Errorf("ops[%d].Kind = %v, want OpSubstitute", i, op.Kind)
		}
	}
}

// TestAlignWeightedJoel mirrors the real, confirmed Joel chapter-size
// pattern: canonical [20,32,21] vs edition [20,27,5,21] - canonical
// chapter 2 (32) divides into edition chapters 2+3 (27+5=32, an exact
// size match, cost 0), not a coincidental same-number substitution.
func TestAlignWeightedJoel(t *testing.T) {
	ops := align.AlignWeighted([]int{20, 32, 21}, []int{20, 27, 5, 21})
	want := []align.Op{
		{Kind: align.OpSubstitute, AIdx: []int{0}, BIdx: []int{0}},
		{Kind: align.OpDivide, AIdx: []int{1}, BIdx: []int{1, 2}},
		{Kind: align.OpSubstitute, AIdx: []int{2}, BIdx: []int{3}},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("AlignWeighted(Joel) = %+v, want %+v", ops, want)
	}
}

// TestAlignWeightedPsalmMerge mirrors the real, confirmed Psalm 9/10
// pattern: canonical Ps9(20)+Ps10(18) merge into edition's single Ps9(39),
// since 20+18=38 is within 1 of 39 (cost 1 - the title verse) versus a much
// higher cost (30) for treating them as two independent substitutions
// against the next two edition chapters in sequence.
func TestAlignWeightedPsalmMerge(t *testing.T) {
	// canonical: Ps9(20), Ps10(18), Ps11(7)
	// edition:   Ps9(39, the merged chapter), Ps10(7, actually KJV Ps11's content)
	ops := align.AlignWeighted([]int{20, 18, 7}, []int{39, 7})
	want := []align.Op{
		{Kind: align.OpMerge, AIdx: []int{0, 1}, BIdx: []int{0}},
		{Kind: align.OpSubstitute, AIdx: []int{2}, BIdx: []int{1}},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("AlignWeighted(Psalm merge) = %+v, want %+v", ops, want)
	}
}

// TestAlignWeightedInsertAndDelete confirms unmatched items on either side
// (eg Psalm 151, or a genuinely missing chapter) become pure insert/delete
// operations when that is the strictly cheaper option, not forced into a
// bad substitution. Weights are chosen so the answer is unambiguous (not a
// cost tie with some other equally-valid path).
func TestAlignWeightedInsertAndDelete(t *testing.T) {
	// delete(a[0]=5)=5 + substitute(10,10)=0 totals 5, strictly cheaper than
	// substitute(5,10)=5 + delete(a[1]=10)=10 totals 15.
	ops := align.AlignWeighted([]int{5, 10}, []int{10})
	want := []align.Op{
		{Kind: align.OpDelete, AIdx: []int{0}},
		{Kind: align.OpSubstitute, AIdx: []int{1}, BIdx: []int{0}},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("AlignWeighted(delete) = %+v, want %+v", ops, want)
	}

	// substitute(10,10)=0 + insert(b[1]=5)=5 totals 5, strictly cheaper than
	// insert(b[0]=10)=10 + substitute(10,5)=5 totals 15.
	ops2 := align.AlignWeighted([]int{10}, []int{10, 5})
	want2 := []align.Op{
		{Kind: align.OpSubstitute, AIdx: []int{0}, BIdx: []int{0}},
		{Kind: align.OpInsert, BIdx: []int{1}},
	}
	if !reflect.DeepEqual(ops2, want2) {
		t.Errorf("AlignWeighted(insert) = %+v, want %+v", ops2, want2)
	}
}

func TestAlignWeightedOpsCoverEveryItemExactlyOnce(t *testing.T) {
	a := []int{20, 32, 21, 9, 39, 6}
	b := []int{20, 27, 5, 21, 10, 38, 6}
	ops := align.AlignWeighted(a, b)

	seenA := map[int]bool{}
	seenB := map[int]bool{}
	for _, op := range ops {
		for _, ai := range op.AIdx {
			if seenA[ai] {
				t.Fatalf("A index %d consumed more than once", ai)
			}
			seenA[ai] = true
		}
		for _, bi := range op.BIdx {
			if seenB[bi] {
				t.Fatalf("B index %d consumed more than once", bi)
			}
			seenB[bi] = true
		}
	}
	if len(seenA) != len(a) {
		t.Errorf("covered %d of %d A items", len(seenA), len(a))
	}
	if len(seenB) != len(b) {
		t.Errorf("covered %d of %d B items", len(seenB), len(b))
	}
}

func TestAlignWeightedDeterministic(t *testing.T) {
	a := []int{20, 32, 21, 9, 39, 6, 18, 7}
	b := []int{20, 27, 5, 21, 10, 38, 7, 6}
	first := align.AlignWeighted(a, b)
	for i := 0; i < 5; i++ {
		again := align.AlignWeighted(a, b)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("AlignWeighted is non-deterministic: run 0 = %+v, run %d = %+v", first, i, again)
		}
	}
}
