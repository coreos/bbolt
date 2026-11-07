package common

import (
	"fmt"
	"os"
	"slices"
	"unsafe"
)

const PageHeaderSize = unsafe.Sizeof(Page{})

const MinKeysPerPage = 2

const BranchPageElementSize = unsafe.Sizeof(branchPageElement{})
const LeafPageElementSize = unsafe.Sizeof(leafPageElement{})
const pgidSize = unsafe.Sizeof(Pgid(0))

const (
	BranchPageFlag   = 0x01
	LeafPageFlag     = 0x02
	MetaPageFlag     = 0x04
	FreelistPageFlag = 0x10
)

const (
	BucketLeafFlag = 0x01
)

type Pgid uint64

type Page struct {
	id       Pgid
	flags    uint16
	count    uint16
	overflow uint32
}

func NewPage(id Pgid, flags, count uint16, overflow uint32) *Page {
	return &Page{
		id:       id,
		flags:    flags,
		count:    count,
		overflow: overflow,
	}
}

// Typ returns a human-readable page type string used for debugging.
func (p *Page) Typ() string {
	if p.IsBranchPage() {
		return "branch"
	} else if p.IsLeafPage() {
		return "leaf"
	} else if p.IsMetaPage() {
		return "meta"
	} else if p.IsFreelistPage() {
		return "freelist"
	}
	return fmt.Sprintf("unknown<%02x>", p.flags)
}

func (p *Page) IsBranchPage() bool {
	return p.flags == BranchPageFlag
}

func (p *Page) IsLeafPage() bool {
	return p.flags == LeafPageFlag
}

func (p *Page) IsMetaPage() bool {
	return p.flags == MetaPageFlag
}

func (p *Page) IsFreelistPage() bool {
	return p.flags == FreelistPageFlag
}

// IsValidPage checks Page flags correctness, only a single proper flag can be used.
func (p *Page) IsValidPage() bool {
	return p.IsBranchPage() ||
		p.IsLeafPage() ||
		p.IsMetaPage() ||
		p.IsFreelistPage()
}

// Meta returns a pointer to the metadata section of the page.
func (p *Page) Meta() *Meta {
	return (*Meta)(UnsafeAdd(unsafe.Pointer(p), unsafe.Sizeof(*p)))
}

func (p *Page) FastCheck(id Pgid) {
	if p.id != id {
		panic(fmt.Sprintf("assertion failed: Page expected to be: %v, but self identifies as %v", id, p.id))
	}
	// Only one flag of page-type can be set.
	if !p.IsValidPage() {
		panic(fmt.Sprintf("assertion failed: page %v: has unexpected type/flags: %x", p.id, p.flags))
	}
}

// LeafPageElement retrieves the leaf node by index
func (p *Page) LeafPageElement(index uint16) *leafPageElement {
	return (*leafPageElement)(UnsafeIndex(unsafe.Pointer(p), unsafe.Sizeof(*p),
		LeafPageElementSize, int(index)))
}

// LeafPageElements retrieves a list of leaf nodes.
func (p *Page) LeafPageElements() []leafPageElement {
	if p.count == 0 {
		return nil
	}
	data := UnsafeAdd(unsafe.Pointer(p), unsafe.Sizeof(*p))
	elems := unsafe.Slice((*leafPageElement)(data), int(p.count))
	return elems
}

// BranchPageElement retrieves the branch node by index
func (p *Page) BranchPageElement(index uint16) *branchPageElement {
	return (*branchPageElement)(UnsafeIndex(unsafe.Pointer(p), unsafe.Sizeof(*p),
		unsafe.Sizeof(branchPageElement{}), int(index)))
}

// BranchPageElements retrieves a list of branch nodes.
func (p *Page) BranchPageElements() []branchPageElement {
	if p.count == 0 {
		return nil
	}
	data := UnsafeAdd(unsafe.Pointer(p), unsafe.Sizeof(*p))
	elems := unsafe.Slice((*branchPageElement)(data), int(p.count))
	return elems
}

func (p *Page) FreelistPageCount() (int, int) {
	if !p.IsFreelistPage() {
		panic(fmt.Sprintf("assertion failed: can't get freelist page count from a non-freelist page: %2x", p.flags))
	}

	// If the page.count is at the max uint16 value (64k) then it's considered
	// an overflow and the size of the freelist is stored as the first element.
	var idx, count = 0, int(p.count)
	if count == 0xFFFF {
		idx = 1
		c := *(*Pgid)(UnsafeAdd(unsafe.Pointer(p), unsafe.Sizeof(*p)))
		count = int(c)
		if count < 0 {
			panic(fmt.Sprintf("leading element count %d overflows int", c))
		}
	}

	return idx, count
}

func (p *Page) FreelistPageIds() []Pgid {
	if !p.IsFreelistPage() {
		panic(fmt.Sprintf("assertion failed: can't get freelist page IDs from a non-freelist page: %2x", p.flags))
	}

	idx, count := p.FreelistPageCount()

	if count == 0 {
		return nil
	}

	data := UnsafeIndex(unsafe.Pointer(p), unsafe.Sizeof(*p), pgidSize, idx)
	ids := unsafe.Slice((*Pgid)(data), count)

	return ids
}

// dump writes n bytes of the page to STDERR as hex output.
func (p *Page) hexdump(n int) {
	buf := UnsafeByteSlice(unsafe.Pointer(p), 0, 0, n)
	fmt.Fprintf(os.Stderr, "%x\n", buf)
}

func (p *Page) PageElementSize() uintptr {
	if p.IsLeafPage() {
		return LeafPageElementSize
	}
	return BranchPageElementSize
}

func (p *Page) Id() Pgid {
	return p.id
}

func (p *Page) SetId(target Pgid) {
	p.id = target
}

func (p *Page) Flags() uint16 {
	return p.flags
}

func (p *Page) SetFlags(v uint16) {
	p.flags = v
}

func (p *Page) Count() uint16 {
	return p.count
}

func (p *Page) SetCount(target uint16) {
	p.count = target
}

func (p *Page) Overflow() uint32 {
	return p.overflow
}

func (p *Page) SetOverflow(target uint32) {
	p.overflow = target
}

func (p *Page) String() string {
	return fmt.Sprintf("ID: %d, Type: %s, count: %d, overflow: %d", p.id, p.Typ(), p.count, p.overflow)
}

type Pages []*Page

func (s Pages) Len() int           { return len(s) }
func (s Pages) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s Pages) Less(i, j int) bool { return s[i].id < s[j].id }

// branchPageElement represents a node on a branch page.
type branchPageElement struct {
	pos   uint32
	ksize uint32
	pgid  Pgid
}

func (n *branchPageElement) Pos() uint32 {
	return n.pos
}

func (n *branchPageElement) SetPos(v uint32) {
	n.pos = v
}

func (n *branchPageElement) Ksize() uint32 {
	return n.ksize
}

func (n *branchPageElement) SetKsize(v uint32) {
	n.ksize = v
}

func (n *branchPageElement) Pgid() Pgid {
	return n.pgid
}

func (n *branchPageElement) SetPgid(v Pgid) {
	n.pgid = v
}

// Key returns a byte slice of the node key.
func (n *branchPageElement) Key() []byte {
	return UnsafeByteSlice(unsafe.Pointer(n), 0, int(n.pos), int(n.pos)+int(n.ksize))
}

// leafPageElement represents a node on a leaf page.
type leafPageElement struct {
	flags uint32
	pos   uint32
	ksize uint32
	vsize uint32
}

func NewLeafPageElement(flags, pos, ksize, vsize uint32) *leafPageElement {
	return &leafPageElement{
		flags: flags,
		pos:   pos,
		ksize: ksize,
		vsize: vsize,
	}
}

func (n *leafPageElement) Flags() uint32 {
	return n.flags
}

func (n *leafPageElement) SetFlags(v uint32) {
	n.flags = v
}

func (n *leafPageElement) Pos() uint32 {
	return n.pos
}

func (n *leafPageElement) SetPos(v uint32) {
	n.pos = v
}

func (n *leafPageElement) Ksize() uint32 {
	return n.ksize
}

func (n *leafPageElement) SetKsize(v uint32) {
	n.ksize = v
}

func (n *leafPageElement) Vsize() uint32 {
	return n.vsize
}

func (n *leafPageElement) SetVsize(v uint32) {
	n.vsize = v
}

// Key returns a byte slice of the node key.
func (n *leafPageElement) Key() []byte {
	i := int(n.pos)
	j := i + int(n.ksize)
	return UnsafeByteSlice(unsafe.Pointer(n), 0, i, j)
}

// Value returns a byte slice of the node value.
func (n *leafPageElement) Value() []byte {
	i := int(n.pos) + int(n.ksize)
	j := i + int(n.vsize)
	return UnsafeByteSlice(unsafe.Pointer(n), 0, i, j)
}

func (n *leafPageElement) IsBucketEntry() bool {
	return n.flags&uint32(BucketLeafFlag) != 0
}

func (n *leafPageElement) Bucket() *InBucket {
	if n.IsBucketEntry() {
		return LoadBucket(n.Value())
	} else {
		return nil
	}
}

// PageInfo represents human readable information about a page.
type PageInfo struct {
	ID            int
	Type          string
	Count         int
	OverflowCount int
}

type Pgids []Pgid

func (s Pgids) Len() int           { return len(s) }
func (s Pgids) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s Pgids) Less(i, j int) bool { return s[i] < s[j] }

// Merge returns the sorted union of a and b, reusing the backing array of s
// when it has enough spare capacity. The backing array of s is overwritten,
// so the returned slice must be used instead of s. Both inputs must be sorted
// and must not contain the same page ID or alias each other.
func (s Pgids) Merge(b Pgids) Pgids {
	// Return the opposite slice if one is nil.
	if len(s) == 0 {
		return b
	}
	if len(b) == 0 {
		return s
	}

	total := len(s) + len(b)
	merged := slices.Grow(s, len(b))[:total]
	mergepgidsInPlace(merged, s, b)
	return merged
}

// Mergepgids copies the sorted union of a and b into dst.
// The inputs must be sorted and must not contain the same page ID.
// The dst must not overlap either input. If dst is too small, it panics.
func Mergepgids(dst, a, b Pgids) {
	if len(dst) < len(a)+len(b) {
		panic(fmt.Errorf("mergepgids bad len %d < %d + %d", len(dst), len(a), len(b)))
	}

	i, j, k := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			dst[k] = a[i]
			i++
		} else {
			dst[k] = b[j]
			j++
		}
		k++
	}

	if i < len(a) {
		copy(dst[k:], a[i:])
		return
	}
	copy(dst[k:], b[j:])
}

func mergepgidsInPlace(dst, a, b Pgids) {
	i, j := len(a), len(b)
	for k := len(dst); k > 0; {
		k--
		switch {
		case i == 0:
			dst[k] = b[j-1]
			j--
		case j == 0 || a[i-1] > b[j-1]:
			dst[k] = a[i-1]
			i--
		default:
			dst[k] = b[j-1]
			j--
		}
	}
}
