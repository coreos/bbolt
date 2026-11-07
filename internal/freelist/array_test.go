package freelist

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"go.etcd.io/bbolt/internal/common"
)

// Ensure that a freelist can find contiguous blocks of pages.
func TestFreelistArray_allocate(t *testing.T) {
	f := NewArrayFreelist()
	ids := []common.Pgid{3, 4, 5, 6, 7, 9, 12, 13, 18}
	f.Init(ids)
	if id := int(f.Allocate(1, 3)); id != 3 {
		t.Fatalf("exp=3; got=%v", id)
	}
	if id := int(f.Allocate(1, 1)); id != 6 {
		t.Fatalf("exp=6; got=%v", id)
	}
	if id := int(f.Allocate(1, 3)); id != 0 {
		t.Fatalf("exp=0; got=%v", id)
	}
	if id := int(f.Allocate(1, 2)); id != 12 {
		t.Fatalf("exp=12; got=%v", id)
	}
	if id := int(f.Allocate(1, 1)); id != 7 {
		t.Fatalf("exp=7; got=%v", id)
	}
	if id := int(f.Allocate(1, 0)); id != 0 {
		t.Fatalf("exp=0; got=%v", id)
	}
	if id := int(f.Allocate(1, 0)); id != 0 {
		t.Fatalf("exp=0; got=%v", id)
	}
	if exp := common.Pgids([]common.Pgid{9, 18}); !reflect.DeepEqual(exp, f.freePageIds()) {
		t.Fatalf("exp=%v; got=%v", exp, f.freePageIds())
	}

	if id := int(f.Allocate(1, 1)); id != 9 {
		t.Fatalf("exp=9; got=%v", id)
	}
	if id := int(f.Allocate(1, 1)); id != 18 {
		t.Fatalf("exp=18; got=%v", id)
	}
	if id := int(f.Allocate(1, 1)); id != 0 {
		t.Fatalf("exp=0; got=%v", id)
	}
	if exp := common.Pgids([]common.Pgid{}); !reflect.DeepEqual(exp, f.freePageIds()) {
		t.Fatalf("exp=%v; got=%v", exp, f.freePageIds())
	}
}

func TestInvalidArrayAllocation(t *testing.T) {
	f := NewArrayFreelist()
	// page 0 and 1 are reserved for meta pages, so they should never be free pages.
	ids := []common.Pgid{1}
	f.Init(ids)
	require.Panics(t, func() {
		f.Allocate(common.Txid(1), 1)
	})
}

func Test_Freelist_Array_Rollback(t *testing.T) {
	f := newTestArrayFreelist()

	f.Init([]common.Pgid{3, 5, 6, 7, 12, 13})

	f.Free(100, common.NewPage(20, 0, 0, 1))
	f.Allocate(100, 3)
	f.Free(100, common.NewPage(25, 0, 0, 0))
	f.Allocate(100, 2)

	require.Equal(t, map[common.Pgid]common.Txid{5: 100, 12: 100}, f.allocs)
	require.Equal(t, map[common.Txid]*txPending{100: {
		ids:     []common.Pgid{20, 21, 25},
		alloctx: []common.Txid{0, 0, 0},
	}}, f.pending)

	f.Rollback(100)

	require.Equal(t, map[common.Pgid]common.Txid{}, f.allocs)
	require.Equal(t, map[common.Txid]*txPending{}, f.pending)
}

func TestFreelistArray_mergeSpans_reusesCapacity(t *testing.T) {
	backing := make(common.Pgids, 2, 4)
	backing[0], backing[1] = 3, 8

	f := newTestArrayFreelist()
	f.Init(backing)
	f.mergeSpans(common.Pgids{5, 9})

	require.Equal(t, common.Pgids{3, 5, 8, 9}, f.freePageIds())
	require.Equal(t, 4, f.FreeCount())
	require.Same(t, &backing[0], &f.freePageIds()[0])
}

func newTestArrayFreelist() *array {
	f := NewArrayFreelist()
	return f.(*array)
}

func benchmarkArrayInitAndMergePgids(maxPgid, mergeCount, mergeSpanLen int, spareCapacity int) (common.Pgids, common.Pgids) {
	const gapSize = 100

	mergeSpanCount := (mergeCount + mergeSpanLen - 1) / mergeSpanLen
	requiredPageCount := mergeCount + mergeSpanCount - 1
	if requiredPageCount > maxPgid-1 {
		panic("invalid benchmark parameters: insufficient pgid space")
	}

	freeCount := maxPgid - mergeCount - mergeSpanCount + 1
	initIDs := make(common.Pgids, 0, freeCount+spareCapacity)
	mergePgids := make(common.Pgids, 0, mergeCount)
	next := common.Pgid(2)
	remaining := mergeCount

	for remaining > 0 {
		spanLen := min(mergeSpanLen, remaining)
		for i := range spanLen {
			mergePgids = append(mergePgids, next+common.Pgid(i))
		}

		next += common.Pgid(spanLen)
		remaining -= spanLen
		if remaining == 0 {
			break
		}

		for range gapSize {
			initIDs = append(initIDs, next)
			next++
		}
	}

	for ; next <= common.Pgid(maxPgid); next++ {
		initIDs = append(initIDs, next)
	}

	return initIDs, mergePgids
}

func benchmarkArrayMergeSpans(b *testing.B, initIDs, mergePgids common.Pgids) {
	b.ReportAllocs()
	mergeIDs := append(common.Pgids(nil), mergePgids...)
	for b.Loop() {
		b.StopTimer()
		f := newTestArrayFreelist()
		f.Init(initIDs)
		b.StartTimer()

		f.mergeSpans(mergeIDs)
		b.StopTimer()
		require.Equal(b, len(initIDs)+len(mergeIDs), f.FreeCount())
		b.StartTimer()
	}
}

func Benchmark_freelist_arrayMergeSpans(b *testing.B) {
	testCases := []struct {
		maxPgid    int
		mergeCount int
	}{
		{maxPgid: 1000, mergeCount: 500},
		{maxPgid: 5000, mergeCount: 2500},
		{maxPgid: 10000, mergeCount: 5000},
	}

	for _, tc := range testCases {
		for _, mergeSpanLen := range []int{1, 16, 32, 64} {
			name := fmt.Sprintf("max%d_merge%d_span%d", tc.maxPgid, tc.mergeCount, mergeSpanLen)
			coldInit, mergePgids := benchmarkArrayInitAndMergePgids(tc.maxPgid, tc.mergeCount, mergeSpanLen, 0)
			warmInit, _ := benchmarkArrayInitAndMergePgids(tc.maxPgid, tc.mergeCount, mergeSpanLen, tc.mergeCount)

			b.Run(name, func(b *testing.B) {
				b.Run("cold", func(b *testing.B) {
					benchmarkArrayMergeSpans(b, coldInit, mergePgids)
				})
				b.Run("warm", func(b *testing.B) {
					benchmarkArrayMergeSpans(b, warmInit, mergePgids)
				})
			})
		}
	}
}
