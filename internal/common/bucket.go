package common

import (
	"fmt"
	"unsafe"
)

const BucketHeaderSize = int(unsafe.Sizeof(InBucket{}))

// InBucket represents the on-file representation of a bucket.
// This is stored as the "value" of a bucket key. If the bucket is small enough,
// then its root page can be stored inline in the "value", after the bucket
// header. In the case of inline buckets, the "root" will be 0.
type InBucket struct {
	root     Pgid   // page id of the bucket's root-level page
	sequence uint64 // monotonically incrementing, used by NextSequence()
}

func NewInBucket(root Pgid, seq uint64) InBucket {
	return InBucket{
		root:     root,
		sequence: seq,
	}
}

func (b *InBucket) RootPage() Pgid {
	return b.root
}

func (b *InBucket) SetRootPage(id Pgid) {
	b.root = id
}

// InSequence returns the sequence. The reason why not naming it `Sequence`
// is to avoid duplicated name as `(*Bucket) Sequence()`
func (b *InBucket) InSequence() uint64 {
	return b.sequence
}

func (b *InBucket) SetInSequence(v uint64) {
	b.sequence = v
}

func (b *InBucket) IncSequence() {
	b.sequence++
}

// InlinePage returns the inline page stored after the bucket header.
// A short buffer returns nil. An unaligned address is copied into an
// 8-byte-aligned backing store before the cast. make([]byte) is only aligned
// for byte, so the copy is allocated as []uint64.
// The aligned fast path still aliases the caller's bytes.
func (b *InBucket) InlinePage(v []byte) *Page {
	if len(v) < BucketHeaderSize+int(PageHeaderSize) {
		return nil
	}
	pageBytes := v[BucketHeaderSize:]
	if uintptr(unsafe.Pointer(&pageBytes[0]))%unsafe.Alignof(Page{}) != 0 {
		return pageFromAlignedCopy(pageBytes)
	}
	return (*Page)(unsafe.Pointer(&v[BucketHeaderSize]))
}

func pageFromAlignedCopy(src []byte) *Page {
	n := (len(src) + 7) / 8
	words := make([]uint64, n)
	dst := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), n*8)
	copy(dst, src)
	return (*Page)(unsafe.Pointer(&words[0]))
}

func (b *InBucket) String() string {
	return fmt.Sprintf("<pgid=%d,seq=%d>", b.root, b.sequence)
}
