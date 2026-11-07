package common

import (
	"reflect"
	"sort"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/require"
)

// Ensure that the page type can be returned in human readable format.
func TestPage_typ(t *testing.T) {
	if typ := (&Page{flags: BranchPageFlag}).Typ(); typ != "branch" {
		t.Fatalf("exp=branch; got=%v", typ)
	}
	if typ := (&Page{flags: LeafPageFlag}).Typ(); typ != "leaf" {
		t.Fatalf("exp=leaf; got=%v", typ)
	}
	if typ := (&Page{flags: MetaPageFlag}).Typ(); typ != "meta" {
		t.Fatalf("exp=meta; got=%v", typ)
	}
	if typ := (&Page{flags: FreelistPageFlag}).Typ(); typ != "freelist" {
		t.Fatalf("exp=freelist; got=%v", typ)
	}
	if typ := (&Page{flags: 20000}).Typ(); typ != "unknown<4e20>" {
		t.Fatalf("exp=unknown<4e20>; got=%v", typ)
	}
}

// Ensure that the hexdump debugging function doesn't blow up.
func TestPage_dump(t *testing.T) {
	(&Page{id: 256}).hexdump(16)
}

func TestPgids_merge(t *testing.T) {
	a := Pgids{4, 5, 6, 10, 11, 12, 13, 27}
	b := Pgids{1, 3, 8, 9, 25, 30}
	c := a.Merge(b)
	if !reflect.DeepEqual(c, Pgids{1, 3, 4, 5, 6, 8, 9, 10, 11, 12, 13, 25, 27, 30}) {
		t.Errorf("mismatch: %v", c)
	}

	a = Pgids{4, 5, 6, 10, 11, 12, 13, 27, 35, 36}
	b = Pgids{8, 9, 25, 30}
	c = a.Merge(b)
	if !reflect.DeepEqual(c, Pgids{4, 5, 6, 8, 9, 10, 11, 12, 13, 25, 27, 30, 35, 36}) {
		t.Errorf("mismatch: %v", c)
	}
}

func TestPgids_merge_quick(t *testing.T) {
	if err := quick.Check(func(a, b Pgids) bool {
		// Sort incoming lists.
		sort.Sort(a)
		sort.Sort(b)

		// Merge the two lists together.
		got := a.Merge(b)

		// The expected value should be the two lists combined and sorted.
		exp := append(a, b...)
		sort.Sort(exp)

		if !reflect.DeepEqual(exp, got) {
			t.Errorf("\nexp=%+v\ngot=%+v\n", exp, got)
			return false
		}

		return true
	}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPgids_mergeInPlace(t *testing.T) {
	tests := []struct {
		name string
		a    Pgids
		b    Pgids
		want Pgids
	}{
		{name: "interleaved", a: Pgids{2, 5, 8}, b: Pgids{3, 4, 9}, want: Pgids{2, 3, 4, 5, 8, 9}},
		{name: "leading", a: Pgids{2, 3}, b: Pgids{10, 12}, want: Pgids{2, 3, 10, 12}},
		{name: "trailing", a: Pgids{10, 12}, b: Pgids{2, 3}, want: Pgids{2, 3, 10, 12}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backing := make(Pgids, len(tt.a), len(tt.a)+len(tt.b))
			copy(backing, tt.a)

			got := backing.Merge(tt.b)
			require.Equal(t, tt.want, got)
			if len(tt.a) != 0 && len(tt.b) != 0 && &got[0] != &backing[0] {
				t.Fatal("Merge did not reuse the destination backing array")
			}
		})
	}
}

var benchmarkPgidsMergeSink Pgids

func benchmarkPgids(size int) (Pgids, Pgids) {
	a := make(Pgids, 0, size)
	ids := make(Pgids, 0, size)
	for i := range 2 * size {
		id := Pgid(2 + 2*i)
		if i%2 == 0 {
			a = append(a, id)
		} else {
			ids = append(ids, id)
		}
	}

	return a, ids
}

func BenchmarkPgids_merge(b *testing.B) {
	const size = 10000
	a, ids := benchmarkPgids(size)

	b.ReportAllocs()
	for b.Loop() {
		benchmarkPgidsMergeSink = a.Merge(ids)
	}
}

func BenchmarkPgids_mergeInPlace(b *testing.B) {
	const size = 10000
	srcA, srcB := benchmarkPgids(size)
	dst := make(Pgids, 0, 2*size)

	b.ReportAllocs()
	for b.Loop() {
		s := dst[:size]
		copy(s, srcA)
		benchmarkPgidsMergeSink = s.Merge(srcB)
	}
}
