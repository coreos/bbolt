package freelist

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"reflect"
	"slices"
	"sort"
	"testing"
	"testing/quick"
	"unsafe"

	"github.com/stretchr/testify/require"

	"go.etcd.io/bbolt/internal/common"
)

// TestFreelistType is used as a env variable for test to indicate the backend type
const TestFreelistType = "TEST_FREELIST_TYPE"

// Ensure that a page is added to a transaction's freelist.
func TestFreelist_free(t *testing.T) {
	f := newTestFreelist()
	f.Free(100, common.NewPage(12, 0, 0, 0))
	if !reflect.DeepEqual([]common.Pgid{12}, f.pendingPageIds()[100].ids) {
		t.Fatalf("exp=%v; got=%v", []common.Pgid{12}, f.pendingPageIds()[100].ids)
	}
}

// Ensure that a page and its overflow is added to a transaction's freelist.
func TestFreelist_free_overflow(t *testing.T) {
	f := newTestFreelist()
	f.Free(100, common.NewPage(12, 0, 0, 3))
	if exp := []common.Pgid{12, 13, 14, 15}; !reflect.DeepEqual(exp, f.pendingPageIds()[100].ids) {
		t.Fatalf("exp=%v; got=%v", exp, f.pendingPageIds()[100].ids)
	}
}

// Ensure that double freeing a page is causing a panic
func TestFreelist_free_double_free_panics(t *testing.T) {
	f := newTestFreelist()
	f.Free(100, common.NewPage(12, 0, 0, 3))
	require.Panics(t, func() {
		f.Free(100, common.NewPage(12, 0, 0, 3))
	})
}

// Ensure that attempting to free the meta page panics
func TestFreelist_free_meta_panics(t *testing.T) {
	f := newTestFreelist()
	require.Panics(t, func() {
		f.Free(100, common.NewPage(0, 0, 0, 0))
	})
	require.Panics(t, func() {
		f.Free(100, common.NewPage(1, 0, 0, 0))
	})
}

func TestFreelist_free_freelist(t *testing.T) {
	f := newTestFreelist()
	f.Free(100, common.NewPage(12, common.FreelistPageFlag, 0, 0))
	pp := f.pendingPageIds()[100]
	require.Equal(t, []common.Pgid{12}, pp.ids)
	require.Equal(t, []common.Txid{0}, pp.alloctx)
}

func TestFreelist_free_freelist_alloctx(t *testing.T) {
	f := newTestFreelist()
	f.Free(100, common.NewPage(12, common.FreelistPageFlag, 0, 0))
	f.Rollback(100)
	require.Empty(t, f.freePageIds())
	require.Empty(t, f.pendingPageIds())
	require.False(t, f.Freed(12))

	f.Free(101, common.NewPage(12, common.FreelistPageFlag, 0, 0))
	require.True(t, f.Freed(12))
	if exp := []common.Pgid{12}; !reflect.DeepEqual(exp, f.pendingPageIds()[101].ids) {
		t.Fatalf("exp=%v; got=%v", exp, f.pendingPageIds()[101].ids)
	}
	f.ReleasePendingPages()
	require.True(t, f.Freed(12))
	require.Empty(t, f.pendingPageIds())
	if exp := common.Pgids([]common.Pgid{12}); !reflect.DeepEqual(exp, f.freePageIds()) {
		t.Fatalf("exp=%v; got=%v", exp, f.freePageIds())
	}
}

// Ensure that a transaction's free pages can be released through ReleasePendingPages().
func TestFreelist_ReleasePendingPages_ReleaseUpTo(t *testing.T) {
	f := newTestFreelist()
	f.Free(100, common.NewPage(12, 0, 0, 1))
	f.Free(100, common.NewPage(9, 0, 0, 0))
	f.Free(102, common.NewPage(39, 0, 0, 0))

	releaseUpTo := func(txid common.Txid) {
		f.AddReadonlyTXID(txid)
		f.ReleasePendingPages()
		f.RemoveReadonlyTXID(txid)
	}

	releaseUpTo(101)
	if exp := common.Pgids([]common.Pgid{9, 12, 13}); !reflect.DeepEqual(exp, f.freePageIds()) {
		t.Fatalf("exp=%v; got=%v", exp, f.freePageIds())
	}

	releaseUpTo(103)
	if exp := common.Pgids([]common.Pgid{9, 12, 13, 39}); !reflect.DeepEqual(exp, f.freePageIds()) {
		t.Fatalf("exp=%v; got=%v", exp, f.freePageIds())
	}
}

// Ensure that pending pages are released through ReleasePendingPages() only when safe.
func TestFreelist_ReleasePendingPages_BoundaryCases(t *testing.T) {
	type testPage struct {
		id       common.Pgid
		n        int
		allocTxn common.Txid
		freeTxn  common.Txid
	}

	var releasePendingTests = []struct {
		title       string
		pagesIn     []testPage
		readonlyTxs []common.Txid
		wantFree    []common.Pgid
	}{
		{
			title:       "release pages when no readonly transaction exists",
			pagesIn:     []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			readonlyTxs: []common.Txid{},
			wantFree:    []common.Pgid{3},
		},
		{
			title:       "readonly transaction before allocation is safe",
			pagesIn:     []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			readonlyTxs: []common.Txid{99},
			wantFree:    []common.Pgid{3},
		},
		{
			title:       "readonly transaction at allocTxn blocks release",
			pagesIn:     []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			readonlyTxs: []common.Txid{100},
			wantFree:    []common.Pgid{},
		},
		{
			title:       "readonly transaction just before freeTxn blocks release",
			pagesIn:     []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			readonlyTxs: []common.Txid{199},
			wantFree:    []common.Pgid{},
		},
		{
			title:       "boundary at freeTxn is safe",
			pagesIn:     []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			readonlyTxs: []common.Txid{200},
			wantFree:    []common.Pgid{3},
		},
		{
			title: "release only pages outside readonly transaction range",
			pagesIn: []testPage{
				{id: 3, n: 1, allocTxn: 100, freeTxn: 200},
				{id: 4, n: 1, allocTxn: 100, freeTxn: 125},
				{id: 5, n: 1, allocTxn: 125, freeTxn: 150},
				{id: 6, n: 1, allocTxn: 125, freeTxn: 175},
				{id: 7, n: 2, allocTxn: 150, freeTxn: 175},
				{id: 9, n: 2, allocTxn: 175, freeTxn: 200},
			},
			readonlyTxs: []common.Txid{150},
			wantFree:    []common.Pgid{4, 5, 9, 10},
		},
	}

	for _, c := range releasePendingTests {
		t.Run(c.title, func(t *testing.T) {
			f := newTestFreelist()
			var ids []common.Pgid
			for _, p := range c.pagesIn {
				for i := uint64(0); i < uint64(p.n); i++ {
					ids = append(ids, common.Pgid(uint64(p.id)+i))
				}
			}
			f.Init(ids)
			for _, p := range c.pagesIn {
				f.Allocate(p.allocTxn, p.n)
			}

			for _, p := range c.pagesIn {
				f.Free(p.freeTxn, common.NewPage(p.id, 0, 0, uint32(p.n-1)))
			}

			for _, txid := range c.readonlyTxs {
				f.AddReadonlyTXID(txid)
			}
			f.ReleasePendingPages()
			for _, txid := range c.readonlyTxs {
				f.RemoveReadonlyTXID(txid)
			}

			require.Equal(t, common.Pgids(c.wantFree), f.freePageIds())
		})
	}
}

func TestFreelist_ReleasePendingPages_SafeReleasePredicate(t *testing.T) {
	f := newTestFreelist()
	f.pendingPageIds()[200] = &txPending{
		ids:     []common.Pgid{10, 20},
		alloctx: []common.Txid{100, 150},
	}

	f.AddReadonlyTXID(120)
	f.ReleasePendingPages()

	require.Equal(t, common.Pgids{20}, f.freePageIds())
	require.Equal(t, []common.Pgid{10}, f.pendingPageIds()[200].ids)
}

func TestFreelist_ReleasePendingPages_BeforeAllocationIsSafe(t *testing.T) {
	f := newTestFreelist()

	f.pendingPageIds()[200] = &txPending{
		ids:     []common.Pgid{10},
		alloctx: []common.Txid{100},
	}

	f.AddReadonlyTXID(99)

	f.ReleasePendingPages()

	require.Empty(t, f.pendingPageIds())
	require.Equal(t, common.Pgids{10}, f.freePageIds())
}

func TestFreelist_ReleasePendingPages_ReleasesAllWhenNoReadonlyTransactions(t *testing.T) {
	f := newTestFreelist()
	f.pendingPageIds()[200] = &txPending{
		ids:     []common.Pgid{10, 20},
		alloctx: []common.Txid{100, 150},
	}

	f.ReleasePendingPages()

	require.Empty(t, f.pendingPageIds())
	require.Equal(t, common.Pgids{10, 20}, f.freePageIds())
}

func TestFreelist_ReleasePendingPages_KeepPendingWhenReadTxnIntersectsInFlightPage(t *testing.T) {
	f := newTestFreelist()
	f.pendingPageIds()[200] = &txPending{
		ids:     []common.Pgid{10},
		alloctx: []common.Txid{100},
	}

	f.AddReadonlyTXID(100)
	f.ReleasePendingPages()

	require.Equal(t, 1, f.PendingCount())
	require.Empty(t, f.freePageIds())
}

func TestFreelist_ReleasePendingPages_BoundaryAtFreeTxIsSafe(t *testing.T) {
	f := newTestFreelist()
	f.pendingPageIds()[200] = &txPending{
		ids:     []common.Pgid{10},
		alloctx: []common.Txid{100},
	}

	f.AddReadonlyTXID(200)
	f.ReleasePendingPages()

	require.Empty(t, f.pendingPageIds())
	require.Equal(t, common.Pgids{10}, f.freePageIds())
}

func TestFreeList_init(t *testing.T) {
	buf := make([]byte, 4096)
	f := newTestFreelist()
	f.Init(common.Pgids{5, 6, 8})

	p := common.LoadPage(buf)
	f.Write(p)

	f2 := newTestFreelist()
	f2.Read(p)
	require.Equal(t, common.Pgids{5, 6, 8}, f2.freePageIds())

	// When initializing the freelist with an empty list of page ID,
	// it should reset the freelist page IDs.
	f2.Init([]common.Pgid{})
	require.Equal(t, common.Pgids{}, f2.freePageIds())
}

func TestFreeList_reload(t *testing.T) {
	buf := make([]byte, 4096)
	f := newTestFreelist()
	f.Init(common.Pgids{5, 6, 8})

	p := common.LoadPage(buf)
	f.Write(p)

	f2 := newTestFreelist()
	f2.Read(p)
	require.Equal(t, common.Pgids{5, 6, 8}, f2.freePageIds())

	f2.Free(common.Txid(5), common.NewPage(10, common.LeafPageFlag, 0, 2))

	// reload shouldn't affect the pending list
	f2.Reload(p)

	require.Equal(t, common.Pgids{5, 6, 8}, f2.freePageIds())
	require.Equal(t, []common.Pgid{10, 11, 12}, f2.pendingPageIds()[5].ids)
}

// Ensure that the txIDx swap, less and len are properly implemented
func TestTxidSorting(t *testing.T) {
	require.NoError(t, quick.Check(func(a []uint64) bool {
		var txids []common.Txid
		for _, txid := range a {
			txids = append(txids, common.Txid(txid))
		}

		sort.Sort(txIDx(txids))

		var r []uint64
		for _, txid := range txids {
			r = append(r, uint64(txid))
		}

		if !slices.IsSorted(r) {
			t.Errorf("txids were not sorted correctly=%v", txids)
			return false
		}

		return true
	}, nil))
}

// Ensure that a freelist can deserialize from a freelist page.
func TestFreelist_read(t *testing.T) {
	// Create a page.
	var buf [4096]byte
	page := (*common.Page)(unsafe.Pointer(&buf[0]))
	page.SetFlags(common.FreelistPageFlag)
	page.SetCount(2)

	// Insert 2 page ids.
	ids := (*[3]common.Pgid)(unsafe.Pointer(uintptr(unsafe.Pointer(page)) + unsafe.Sizeof(*page)))
	ids[0] = 23
	ids[1] = 50

	// Deserialize page into a freelist.
	f := newTestFreelist()
	f.Read(page)

	// Ensure that there are two page ids in the freelist.
	if exp := common.Pgids([]common.Pgid{23, 50}); !reflect.DeepEqual(exp, f.freePageIds()) {
		t.Fatalf("exp=%v; got=%v", exp, f.freePageIds())
	}
}

// Ensure that we never read a non-freelist page
func TestFreelist_read_panics(t *testing.T) {
	buf := make([]byte, 4096)
	page := common.LoadPage(buf)
	page.SetFlags(common.BranchPageFlag)
	page.SetCount(2)
	f := newTestFreelist()
	require.Panics(t, func() {
		f.Read(page)
	})
}

// Ensure that a freelist can serialize into a freelist page.
func TestFreelist_write(t *testing.T) {
	// Create a freelist and write it to a page.
	var buf [4096]byte
	f := newTestFreelist()

	f.Init([]common.Pgid{12, 39})
	f.pendingPageIds()[100] = &txPending{ids: []common.Pgid{28, 11}}
	f.pendingPageIds()[101] = &txPending{ids: []common.Pgid{3}}
	p := (*common.Page)(unsafe.Pointer(&buf[0]))
	f.Write(p)

	// Read the page back out.
	f2 := newTestFreelist()
	f2.Read(p)

	// Ensure that the freelist is correct.
	// All pages should be present and in reverse order.
	if exp := common.Pgids([]common.Pgid{3, 11, 12, 28, 39}); !reflect.DeepEqual(exp, f2.freePageIds()) {
		t.Fatalf("exp=%v; got=%v", exp, f2.freePageIds())
	}
}

func TestFreelist_E2E_HappyPath(t *testing.T) {
	f := newTestFreelist()
	f.Init([]common.Pgid{})
	requirePages(t, f, common.Pgids{}, common.Pgids{})

	allocated := f.Allocate(common.Txid(1), 5)
	require.Equal(t, common.Pgid(0), allocated)
	// tx.go may now allocate more space, and eventually we need to delete a page again
	f.Free(common.Txid(2), common.NewPage(5, common.LeafPageFlag, 0, 0))
	f.Free(common.Txid(2), common.NewPage(3, common.LeafPageFlag, 0, 0))
	f.Free(common.Txid(2), common.NewPage(8, common.LeafPageFlag, 0, 0))
	// the above will only mark the pages as pending, so free pages should not return anything
	requirePages(t, f, common.Pgids{}, common.Pgids{3, 5, 8})

	// someone wants to do a read on top of the next tx id
	f.AddReadonlyTXID(common.Txid(3))
	// this should free the above pages for tx 2 entirely
	f.ReleasePendingPages()
	requirePages(t, f, common.Pgids{3, 5, 8}, common.Pgids{})

	// no span of two pages available should yield a zero-page result
	require.Equal(t, common.Pgid(0), f.Allocate(common.Txid(4), 2))
	// we should be able to allocate those pages independently however,
	// map and array differ in the order they return the pages
	expectedPgids := map[common.Pgid]struct{}{3: {}, 5: {}, 8: {}}
	for i := 0; i < 3; i++ {
		allocated = f.Allocate(common.Txid(4), 1)
		require.Contains(t, expectedPgids, allocated, "expected to find pgid %d", allocated)
		require.False(t, f.Freed(allocated))
		delete(expectedPgids, allocated)
	}
	require.Emptyf(t, expectedPgids, "unexpectedly more than one page was still found")
	// no more free pages to allocate
	require.Equal(t, common.Pgid(0), f.Allocate(common.Txid(4), 1))
}

func TestFreelist_E2E_MultiSpanOverflows(t *testing.T) {
	f := newTestFreelist()
	f.Init([]common.Pgid{})
	f.Free(common.Txid(10), common.NewPage(20, common.LeafPageFlag, 0, 1))
	f.Free(common.Txid(10), common.NewPage(25, common.LeafPageFlag, 0, 2))
	f.Free(common.Txid(10), common.NewPage(35, common.LeafPageFlag, 0, 3))
	f.Free(common.Txid(10), common.NewPage(39, common.LeafPageFlag, 0, 2))
	f.Free(common.Txid(10), common.NewPage(45, common.LeafPageFlag, 0, 4))
	requirePages(t, f, common.Pgids{}, common.Pgids{20, 21, 25, 26, 27, 35, 36, 37, 38, 39, 40, 41, 45, 46, 47, 48, 49})
	f.ReleasePendingPages()
	requirePages(t, f, common.Pgids{20, 21, 25, 26, 27, 35, 36, 37, 38, 39, 40, 41, 45, 46, 47, 48, 49}, common.Pgids{})

	// that sequence, regardless of implementation, should always yield the same blocks of pages
	allocSequence := []int{7, 5, 3, 2}
	expectedSpanStarts := []common.Pgid{35, 45, 25, 20}
	for i, pageNums := range allocSequence {
		allocated := f.Allocate(common.Txid(11), pageNums)
		require.Equal(t, expectedSpanStarts[i], allocated)
		// ensure all pages in that span are not considered free anymore
		for i := 0; i < pageNums; i++ {
			require.False(t, f.Freed(allocated+common.Pgid(i)))
		}
	}
}

func TestFreelist_E2E_Rollbacks(t *testing.T) {
	freelist := newTestFreelist()
	freelist.Init([]common.Pgid{})
	freelist.Free(common.Txid(2), common.NewPage(5, common.LeafPageFlag, 0, 1))
	freelist.Free(common.Txid(2), common.NewPage(8, common.LeafPageFlag, 0, 0))
	requirePages(t, freelist, common.Pgids{}, common.Pgids{5, 6, 8})
	freelist.Rollback(common.Txid(2))
	requirePages(t, freelist, common.Pgids{}, common.Pgids{})

	// unknown transaction should not trigger anything
	freelist.Free(common.Txid(4), common.NewPage(13, common.LeafPageFlag, 0, 3))
	requirePages(t, freelist, common.Pgids{}, common.Pgids{13, 14, 15, 16})
	freelist.ReleasePendingPages()
	requirePages(t, freelist, common.Pgids{13, 14, 15, 16}, common.Pgids{})
	freelist.Rollback(common.Txid(1337))
	requirePages(t, freelist, common.Pgids{13, 14, 15, 16}, common.Pgids{})
}

func TestFreelist_E2E_RollbackPanics(t *testing.T) {
	freelist := newTestFreelist()
	freelist.Init([]common.Pgid{5})
	requirePages(t, freelist, common.Pgids{5}, common.Pgids{})

	_ = freelist.Allocate(common.Txid(5), 1)
	require.Panics(t, func() {
		// depending on the verification level, either should panic
		freelist.Free(common.Txid(5), common.NewPage(5, common.LeafPageFlag, 0, 0))
		freelist.Rollback(5)
	})
}

// tests the reloading from another physical page
func TestFreelist_E2E_Reload(t *testing.T) {
	freelist := newTestFreelist()
	freelist.Init([]common.Pgid{})
	freelist.Free(common.Txid(2), common.NewPage(5, common.LeafPageFlag, 0, 1))
	freelist.Free(common.Txid(2), common.NewPage(8, common.LeafPageFlag, 0, 0))
	freelist.ReleasePendingPages()
	requirePages(t, freelist, common.Pgids{5, 6, 8}, common.Pgids{})
	buf := make([]byte, 4096)
	p := common.LoadPage(buf)
	freelist.Write(p)

	freelist.Free(common.Txid(3), common.NewPage(3, common.LeafPageFlag, 0, 1))
	freelist.Free(common.Txid(3), common.NewPage(10, common.LeafPageFlag, 0, 2))
	requirePages(t, freelist, common.Pgids{5, 6, 8}, common.Pgids{3, 4, 10, 11, 12})

	otherBuf := make([]byte, 4096)
	px := common.LoadPage(otherBuf)
	freelist.Write(px)

	loadFreeList := newTestFreelist()
	loadFreeList.Init([]common.Pgid{})
	loadFreeList.Read(px)
	requirePages(t, loadFreeList, common.Pgids{3, 4, 5, 6, 8, 10, 11, 12}, common.Pgids{})
	// restore the original freelist again
	loadFreeList.Reload(p)
	requirePages(t, loadFreeList, common.Pgids{5, 6, 8}, common.Pgids{})

	// reload another page with different free pages to test we are deduplicating the free pages with the pending ones correctly
	freelist = newTestFreelist()
	freelist.Init([]common.Pgid{})
	freelist.Free(common.Txid(5), common.NewPage(5, common.LeafPageFlag, 0, 4))
	freelist.Reload(p)
	requirePages(t, freelist, common.Pgids{}, common.Pgids{5, 6, 7, 8, 9})
}

// tests the loading and reloading from physical pages
func TestFreelist_E2E_SerDe_HappyPath(t *testing.T) {
	freelist := newTestFreelist()
	freelist.Init([]common.Pgid{})
	freelist.Free(common.Txid(2), common.NewPage(5, common.LeafPageFlag, 0, 1))
	freelist.Free(common.Txid(2), common.NewPage(8, common.LeafPageFlag, 0, 0))
	freelist.ReleasePendingPages()
	requirePages(t, freelist, common.Pgids{5, 6, 8}, common.Pgids{})

	freelist.Free(common.Txid(3), common.NewPage(3, common.LeafPageFlag, 0, 1))
	freelist.Free(common.Txid(3), common.NewPage(10, common.LeafPageFlag, 0, 2))
	requirePages(t, freelist, common.Pgids{5, 6, 8}, common.Pgids{3, 4, 10, 11, 12})

	buf := make([]byte, 4096)
	p := common.LoadPage(buf)
	require.Equal(t, 80, freelist.EstimatedWritePageSize())
	freelist.Write(p)

	loadFreeList := newTestFreelist()
	loadFreeList.Init([]common.Pgid{})
	loadFreeList.Read(p)
	requirePages(t, loadFreeList, common.Pgids{3, 4, 5, 6, 8, 10, 11, 12}, common.Pgids{})
}

// tests the loading of a freelist against other implementations with various sizes
func TestFreelist_E2E_SerDe_AcrossImplementations(t *testing.T) {
	testSizes := []int{0, 1, 10, 100, 1000, math.MaxUint16, math.MaxUint16 + 1, math.MaxUint16 * 2}
	for _, size := range testSizes {
		t.Run(fmt.Sprintf("n=%d", size), func(t *testing.T) {
			freelist := newTestFreelist()
			expectedFreePgids := common.Pgids{}
			for i := 0; i < size; i++ {
				pgid := common.Pgid(i + 2)
				freelist.Free(common.Txid(1), common.NewPage(pgid, common.LeafPageFlag, 0, 0))
				expectedFreePgids = append(expectedFreePgids, pgid)
			}
			freelist.ReleasePendingPages()
			requirePages(t, freelist, expectedFreePgids, common.Pgids{})
			buf := make([]byte, freelist.EstimatedWritePageSize())
			p := common.LoadPage(buf)
			freelist.Write(p)

			for n, loadFreeList := range map[string]Interface{
				"hashmap": NewHashMapFreelist(),
				"array":   NewArrayFreelist(),
			} {
				t.Run(n, func(t *testing.T) {
					loadFreeList.Read(p)
					requirePages(t, loadFreeList, expectedFreePgids, common.Pgids{})
				})
			}
		})
	}
}

func requirePages(t *testing.T, f Interface, freePageIds common.Pgids, pendingPageIds common.Pgids) {
	require.Equal(t, f.FreeCount()+f.PendingCount(), f.Count())
	require.Equalf(t, freePageIds, f.freePageIds(), "unexpected free pages")
	require.Equal(t, len(freePageIds), f.FreeCount())

	pp := allPendingPages(f.pendingPageIds())
	require.Equalf(t, pendingPageIds, pp, "unexpected pending pages")
	require.Equal(t, len(pp), f.PendingCount())

	for _, pgid := range f.freePageIds() {
		require.Truef(t, f.Freed(pgid), "expected free page to return true on Freed")
	}

	for _, pgid := range pp {
		require.Truef(t, f.Freed(pgid), "expected pending page to return true on Freed")
	}
}

func allPendingPages(p map[common.Txid]*txPending) common.Pgids {
	pgids := common.Pgids{}
	for _, pending := range p {
		pgids = append(pgids, pending.ids...)
	}
	sort.Sort(pgids)
	return pgids
}

func Benchmark_FreelistRelease10K(b *testing.B)    { benchmark_FreelistRelease(b, 10000) }
func Benchmark_FreelistRelease100K(b *testing.B)   { benchmark_FreelistRelease(b, 100000) }
func Benchmark_FreelistRelease1000K(b *testing.B)  { benchmark_FreelistRelease(b, 1000000) }
func Benchmark_FreelistRelease10000K(b *testing.B) { benchmark_FreelistRelease(b, 10000000) }

func benchmark_FreelistRelease(b *testing.B, size int) {
	ids := randomPgids(size)
	pending := randomPgids(len(ids) / 400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		txp := &txPending{ids: pending}
		f := newTestFreelist()
		f.pendingPageIds()[1] = txp
		f.Init(ids)
		f.AddReadonlyTXID(2)
		f.ReleasePendingPages()
		f.RemoveReadonlyTXID(2)
	}
}

func randomPgids(n int) []common.Pgid {
	pgids := make(common.Pgids, n)
	for i := range pgids {
		pgids[i] = common.Pgid(rand.Int63())
	}
	sort.Sort(pgids)
	return pgids
}

func Test_freelist_ReadIDs_and_getFreePageIDs(t *testing.T) {
	f := newTestFreelist()
	exp := common.Pgids([]common.Pgid{3, 4, 5, 6, 7, 9, 12, 13, 18})

	f.Init(exp)

	if got := f.freePageIds(); !reflect.DeepEqual(exp, got) {
		t.Fatalf("exp=%v; got=%v", exp, got)
	}

	f2 := newTestFreelist()
	exp2 := []common.Pgid{}
	f2.Init(exp2)

	if got2 := f2.freePageIds(); !reflect.DeepEqual(got2, common.Pgids(exp2)) {
		t.Fatalf("exp2=%#v; got2=%#v", exp2, got2)
	}

}

// newTestFreelist get the freelist type from env and initial the freelist
func newTestFreelist() Interface {
	if env := os.Getenv(TestFreelistType); env == "hashmap" {
		return NewHashMapFreelist()
	}

	return NewArrayFreelist()
}
