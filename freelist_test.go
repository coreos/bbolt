package bbolt

import (
	"math/rand"
	"os"
	"reflect"
	"sort"
	"testing"
	"unsafe"
)

// TestFreelistType is used as a env variable for test to indicate the backend type
const TestFreelistType = "TEST_FREELIST_TYPE"

// Ensure that a page is added to a transaction's freelist.
func TestFreelist_free(t *testing.T) {
	f := newTestFreelist()
	f.free(100, &page{id: 12})
	if !reflect.DeepEqual([]pgid{12}, f.pending[100].ids) {
		t.Fatalf("exp=%v; got=%v", []pgid{12}, f.pending[100].ids)
	}
}

// Ensure that a page and its overflow is added to a transaction's freelist.
func TestFreelist_free_overflow(t *testing.T) {
	f := newTestFreelist()
	f.free(100, &page{id: 12, overflow: 3})
	if exp := []pgid{12, 13, 14, 15}; !reflect.DeepEqual(exp, f.pending[100].ids) {
		t.Fatalf("exp=%v; got=%v", exp, f.pending[100].ids)
	}
}

// Ensure that release handles boundary conditions correctly
func TestFreelist_release(t *testing.T) {
	type testPage struct {
		id       pgid
		n        int
		allocTxn txid
		freeTxn  txid
	}

	var releaseTests = []struct {
		title    string
		pagesIn  []testPage
		txidSets [][]txid
		wantFree []pgid
	}{
		{
			title:    "Single pending #1",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{1}},
			wantFree: []pgid{3},
		},
		{
			title:    "Single pending #2",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{300}},
			wantFree: []pgid{3},
		},
		{
			title:    "Single pending #3",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{1, 300}},
			wantFree: []pgid{3},
		},
		{
			title:    "Single pending #4",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{99}},
			wantFree: []pgid{3},
		},
		{
			title:    "Single pending #5",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{200}},
			wantFree: []pgid{3},
		},
		{
			title:    "Single pending #6",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{200, 99}},
			wantFree: []pgid{3},
		},
		{
			title:    "Single pending #7",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{100}},
			wantFree: []pgid{},
		},
		{
			title:    "Single pending #8",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{199}},
			wantFree: []pgid{},
		},
		{
			title:    "Single pending #9",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{100, 199}},
			wantFree: []pgid{},
		},
		{
			title:    "Single pending #10",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{199, 200}},
			wantFree: []pgid{},
		},
		{
			title:    "Single pending #11",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{{100, 99}},
			wantFree: []pgid{},
		},
		{
			title:    "Single pending #12",
			pagesIn:  []testPage{{id: 3, n: 1, allocTxn: 100, freeTxn: 200}},
			txidSets: [][]txid{nil},
			wantFree: []pgid{3},
		},
		{
			title: "Adjacent pending #1",
			pagesIn: []testPage{
				{id: 3, n: 1, allocTxn: 199, freeTxn: 200},
				{id: 4, n: 1, allocTxn: 200, freeTxn: 201},
			},
			txidSets: [][]txid{{100, 198, 200, 199, 201, 300}},
			wantFree: []pgid{},
		},
		{
			title: "Adjacent pending #2",
			pagesIn: []testPage{
				{id: 3, n: 1, allocTxn: 199, freeTxn: 200},
				{id: 4, n: 1, allocTxn: 200, freeTxn: 201},
			},
			txidSets: [][]txid{{201, 199, 201, 200, 200, 200}},
			wantFree: []pgid{},
		},
		{
			title: "Adjacent pending #3",
			pagesIn: []testPage{
				{id: 3, n: 1, allocTxn: 199, freeTxn: 200},
				{id: 4, n: 1, allocTxn: 200, freeTxn: 201},
			},
			txidSets: [][]txid{nil},
			wantFree: []pgid{3, 4},
		},
		{
			title: "Multiple pending #1",
			pagesIn: []testPage{
				{id: 3, n: 1, allocTxn: 100, freeTxn: 200},
				{id: 4, n: 1, allocTxn: 100, freeTxn: 125},
				{id: 5, n: 1, allocTxn: 125, freeTxn: 150},
				{id: 6, n: 1, allocTxn: 125, freeTxn: 175},
				{id: 7, n: 2, allocTxn: 150, freeTxn: 175},
				{id: 9, n: 2, allocTxn: 175, freeTxn: 200},
			},
			txidSets: [][]txid{
				{125, 151, 199},
				{50, 149, 151, 300},
			},
			wantFree: []pgid{4, 9, 10},
		},
		{
			title: "Multiple pending #2",
			pagesIn: []testPage{
				{id: 3, n: 1, allocTxn: 100, freeTxn: 200},
				{id: 4, n: 1, allocTxn: 100, freeTxn: 250},
				{id: 5, n: 1, allocTxn: 125, freeTxn: 200},
				{id: 6, n: 1, allocTxn: 125, freeTxn: 250},
				{id: 7, n: 2, allocTxn: 150, freeTxn: 200},
				{id: 9, n: 2, allocTxn: 175, freeTxn: 250},
			},
			txidSets: [][]txid{
				{110, 240},
				{249},
			},
			wantFree: []pgid{3, 5, 7, 8},
		},
	}

	for _, c := range releaseTests {
		f := newTestFreelist()
		var ids []pgid
		for _, p := range c.pagesIn {
			for i := uint64(0); i < uint64(p.n); i++ {
				ids = append(ids, pgid(uint64(p.id)+i))
			}
		}
		f.readIDs(ids)
		for _, p := range c.pagesIn {
			f.allocate(p.allocTxn, p.n)
		}

		for _, p := range c.pagesIn {
			f.free(p.freeTxn, &page{id: p.id, overflow: uint32(p.n - 1)})
		}

		for _, txids := range c.txidSets {
			f.release(txids)
		}

		if exp := c.wantFree; !reflect.DeepEqual(exp, f.getFreePageIDs()) {
			t.Errorf("exp=%v; got=%v for %s", exp, f.getFreePageIDs(), c.title)
		}
	}
}

func TestFreelistHashmap_allocate(t *testing.T) {
	f := newTestFreelist()
	if f.freelistType != FreelistMapType {
		t.Skip()
	}

	ids := []pgid{3, 4, 5, 6, 7, 9, 12, 13, 18}
	f.readIDs(ids)

	f.allocate(1, 3)
	if x := f.free_count(); x != 6 {
		t.Fatalf("exp=6; got=%v", x)
	}

	f.allocate(1, 2)
	if x := f.free_count(); x != 4 {
		t.Fatalf("exp=4; got=%v", x)
	}
	f.allocate(1, 1)
	if x := f.free_count(); x != 3 {
		t.Fatalf("exp=3; got=%v", x)
	}

	f.allocate(1, 0)
	if x := f.free_count(); x != 3 {
		t.Fatalf("exp=3; got=%v", x)
	}
}

// Ensure that a freelist can find contiguous blocks of pages.
func TestFreelistArray_allocate(t *testing.T) {
	f := newTestFreelist()
	if f.freelistType != FreelistArrayType {
		t.Skip()
	}
	ids := []pgid{3, 4, 5, 6, 7, 9, 12, 13, 18}
	f.readIDs(ids)
	if id := int(f.allocate(1, 3)); id != 3 {
		t.Fatalf("exp=3; got=%v", id)
	}
	if id := int(f.allocate(1, 1)); id != 6 {
		t.Fatalf("exp=6; got=%v", id)
	}
	if id := int(f.allocate(1, 3)); id != 0 {
		t.Fatalf("exp=0; got=%v", id)
	}
	if id := int(f.allocate(1, 2)); id != 12 {
		t.Fatalf("exp=12; got=%v", id)
	}
	if id := int(f.allocate(1, 1)); id != 7 {
		t.Fatalf("exp=7; got=%v", id)
	}
	if id := int(f.allocate(1, 0)); id != 0 {
		t.Fatalf("exp=0; got=%v", id)
	}
	if id := int(f.allocate(1, 0)); id != 0 {
		t.Fatalf("exp=0; got=%v", id)
	}
	if exp := []pgid{9, 18}; !reflect.DeepEqual(exp, f.getFreePageIDs()) {
		t.Fatalf("exp=%v; got=%v", exp, f.getFreePageIDs())
	}

	if id := int(f.allocate(1, 1)); id != 9 {
		t.Fatalf("exp=9; got=%v", id)
	}
	if id := int(f.allocate(1, 1)); id != 18 {
		t.Fatalf("exp=18; got=%v", id)
	}
	if id := int(f.allocate(1, 1)); id != 0 {
		t.Fatalf("exp=0; got=%v", id)
	}
	if exp := []pgid{}; !reflect.DeepEqual(exp, f.getFreePageIDs()) {
		t.Fatalf("exp=%v; got=%v", exp, f.getFreePageIDs())
	}
}

// Ensure that a freelist can deserialize from a freelist page.
func TestFreelist_read(t *testing.T) {
	// Create a page.
	var buf [4096]byte
	page := (*page)(unsafe.Pointer(&buf[0]))
	page.flags = freelistPageFlag
	page.count = 2

	// Insert 2 page ids.
	ids := (*[3]pgid)(unsafe.Pointer(uintptr(unsafe.Pointer(page)) + unsafe.Sizeof(*page)))
	ids[0] = 23
	ids[1] = 50

	// Deserialize page into a freelist.
	f := newTestFreelist()
	f.read(page)

	// Ensure that there are two page ids in the freelist.
	if exp := []pgid{23, 50}; !reflect.DeepEqual(exp, f.getFreePageIDs()) {
		t.Fatalf("exp=%v; got=%v", exp, f.getFreePageIDs())
	}
}

// Ensure that a freelist can serialize into a freelist page.
func TestFreelist_write(t *testing.T) {
	// Create a freelist and write it to a page.
	var buf [4096]byte
	f := newTestFreelist()

	f.readIDs([]pgid{12, 39})
	f.pending[100] = &txPending{ids: []pgid{28, 11}}
	f.pending[101] = &txPending{ids: []pgid{3}}
	p := (*page)(unsafe.Pointer(&buf[0]))
	if err := f.write(p); err != nil {
		t.Fatal(err)
	}

	// Read the page back out.
	f2 := newTestFreelist()
	f2.read(p)

	// Ensure that the freelist is correct.
	// All pages should be present and in reverse order.
	if exp := []pgid{3, 11, 12, 28, 39}; !reflect.DeepEqual(exp, f2.getFreePageIDs()) {
		t.Fatalf("exp=%v; got=%v", exp, f2.getFreePageIDs())
	}
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
		f.pending = map[txid]*txPending{1: txp}
		f.readIDs(ids)
		f.release([]txid{1})
	}
}

func randomPgids(n int) []pgid {
	rand.Seed(42)
	pgids := make(pgids, n)
	for i := range pgids {
		pgids[i] = pgid(rand.Int63())
	}
	sort.Sort(pgids)
	return pgids
}

func Test_freelist_ReadIDs_and_getFreePageIDs(t *testing.T) {
	f := newTestFreelist()
	exp := []pgid{3, 4, 5, 6, 7, 9, 12, 13, 18}

	f.readIDs(exp)

	if got := f.getFreePageIDs(); !reflect.DeepEqual(exp, got) {
		t.Fatalf("exp=%v; got=%v", exp, got)
	}

	f2 := newTestFreelist()
	var exp2 []pgid
	f2.readIDs(exp2)

	if got2 := f2.getFreePageIDs(); !reflect.DeepEqual(got2, exp2) {
		t.Fatalf("exp2=%#v; got2=%#v", exp2, got2)
	}

}

func Test_freelist_mergeWithExist(t *testing.T) {
	bm1 := pidSet{1: struct{}{}}

	bm2 := pidSet{5: struct{}{}}
	tests := []struct {
		name            string
		ids             []pgid
		pgid            pgid
		want            []pgid
		wantForwardmap  map[pgid]uint64
		wantBackwardmap map[pgid]uint64
		wantfreemap     map[uint64]pidSet
	}{
		{
			name:            "test1",
			ids:             []pgid{1, 2, 4, 5, 6},
			pgid:            3,
			want:            []pgid{1, 2, 3, 4, 5, 6},
			wantForwardmap:  map[pgid]uint64{1: 6},
			wantBackwardmap: map[pgid]uint64{6: 6},
			wantfreemap:     map[uint64]pidSet{6: bm1},
		},
		{
			name:            "test2",
			ids:             []pgid{1, 2, 5, 6},
			pgid:            3,
			want:            []pgid{1, 2, 3, 5, 6},
			wantForwardmap:  map[pgid]uint64{1: 3, 5: 2},
			wantBackwardmap: map[pgid]uint64{6: 2, 3: 3},
			wantfreemap:     map[uint64]pidSet{3: bm1, 2: bm2},
		},
		{
			name:            "test3",
			ids:             []pgid{1, 2},
			pgid:            3,
			want:            []pgid{1, 2, 3},
			wantForwardmap:  map[pgid]uint64{1: 3},
			wantBackwardmap: map[pgid]uint64{3: 3},
			wantfreemap:     map[uint64]pidSet{3: bm1},
		},
		{
			name:            "test4",
			ids:             []pgid{2, 3},
			pgid:            1,
			want:            []pgid{1, 2, 3},
			wantForwardmap:  map[pgid]uint64{1: 3},
			wantBackwardmap: map[pgid]uint64{3: 3},
			wantfreemap:     map[uint64]pidSet{3: bm1},
		},
	}
	for _, tt := range tests {
		f := newTestFreelist()
		if f.freelistType == FreelistArrayType {
			t.Skip()
		}
		f.readIDs(tt.ids)

		f.mergeWithExistingSpan(tt.pgid)

		if got := f.getFreePageIDs(); !reflect.DeepEqual(tt.want, got) {
			t.Fatalf("name %s; exp=%v; got=%v", tt.name, tt.want, got)
		}
		if got := f.forwardMap; !reflect.DeepEqual(tt.wantForwardmap, got) {
			t.Fatalf("name %s; exp=%v; got=%v", tt.name, tt.wantForwardmap, got)
		}
		if got := f.backwardMap; !reflect.DeepEqual(tt.wantBackwardmap, got) {
			t.Fatalf("name %s; exp=%v; got=%v", tt.name, tt.wantBackwardmap, got)
		}
		if got := f.freemaps; !reflect.DeepEqual(tt.wantfreemap, got) {
			t.Fatalf("name %s; exp=%v; got=%v", tt.name, tt.wantfreemap, got)
		}
	}
}

// newTestFreelist get the freelist type from env and initial the freelist
func newTestFreelist() *freelist {
	freelistType := FreelistArrayType
	if env := os.Getenv(TestFreelistType); env == string(FreelistMapType) {
		freelistType = FreelistMapType
	}

	return newFreelist(freelistType)
}
