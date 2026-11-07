package common

import (
	"encoding/binary"
	"reflect"
	"sort"
	"testing"
	"testing/quick"
	"unsafe"
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

func TestLoadBucket_UnalignedOddKey(t *testing.T) {
	words := make([]uint64, 16)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*8)
	p := LoadPage(buf)
	p.SetFlags(LeafPageFlag)
	p.SetCount(1)

	elem := p.LeafPageElement(0)
	elem.SetFlags(uint32(BucketLeafFlag))
	elem.SetKsize(1)
	elem.SetVsize(uint32(BucketHeaderSize))
	elem.SetPos(uint32(LeafPageElementSize))

	key := elem.Key()
	if len(key) != 1 {
		t.Fatalf("key len=%d", len(key))
	}
	key[0] = 'a'

	val := elem.Value()
	if len(val) != BucketHeaderSize {
		t.Fatalf("value len=%d", len(val))
	}
	if uintptr(unsafe.Pointer(&val[0]))%unsafe.Alignof(InBucket{}) == 0 {
		t.Fatal("fixture is aligned; odd key length did not shift the header")
	}
	binary.NativeEndian.PutUint64(val[0:8], 7)
	binary.NativeEndian.PutUint64(val[8:16], 9)

	bkt := elem.Bucket()
	if bkt == nil {
		t.Fatal("LoadBucket returned nil for a full header")
	}
	if bkt.RootPage() != 7 || bkt.InSequence() != 9 {
		t.Fatalf("got root=%d seq=%d", bkt.RootPage(), bkt.InSequence())
	}
}

func TestLoadBucket_ShortBuffer(t *testing.T) {
	if LoadBucket(nil) != nil {
		t.Fatal("nil buffer")
	}
	if LoadBucket([]byte{1, 2, 3}) != nil {
		t.Fatal("short buffer")
	}
}

func TestInlinePage_ShortAndUnaligned(t *testing.T) {
	b := NewInBucket(0, 1)
	if b.InlinePage(nil) != nil {
		t.Fatal("nil value")
	}
	if b.InlinePage(make([]byte, BucketHeaderSize)) != nil {
		t.Fatal("header without page")
	}

	words := make([]uint64, 16)
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*8)
	v := raw[1 : 1+BucketHeaderSize+int(PageHeaderSize)+8]
	if uintptr(unsafe.Pointer(&v[BucketHeaderSize]))%unsafe.Alignof(Page{}) == 0 {
		t.Fatal("fixture page is aligned")
	}
	binary.NativeEndian.PutUint64(v[BucketHeaderSize:], 3)
	binary.NativeEndian.PutUint16(v[BucketHeaderSize+8:], LeafPageFlag)

	p := b.InlinePage(v)
	if p == nil {
		t.Fatal("expected inline page")
	}
	if p.Id() != 3 {
		t.Fatalf("id=%d", p.Id())
	}
	if !p.IsLeafPage() {
		t.Fatalf("flags=%x", p.Flags())
	}
}
