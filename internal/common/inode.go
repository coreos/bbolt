package common

import (
	"bytes"
	"unsafe"
)

// Inode represents an internal node inside of a node.
// It can be used to point to elements in a page or point
// to an element which hasn't been added to a page yet.
type Inode struct {
	flags uint32
	pgid  Pgid
	key   []byte
	value []byte
}

type Inodes []Inode

// Search returns the lowest index i where inodes[i].Key() >= key.
func (inodes Inodes) Search(key []byte) int {
	low, high := 0, len(inodes)
	for low < high {
		mid := int(uint(low+high) >> 1)
		if bytes.Compare(inodes[mid].Key(), key) < 0 {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return low
}

// SearchExact returns the index and whether an exact match was found.
func (inodes Inodes) SearchExact(key []byte) (int, bool) {
	low, high := 0, len(inodes)
	exact := false
	for low < high {
		mid := int(uint(low+high) >> 1)
		ret := bytes.Compare(inodes[mid].Key(), key)
		if ret == 0 {
			exact = true
			low = mid
			break
		} else if ret < 0 {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return low, exact
}

func (in *Inode) Flags() uint32 {
	return in.flags
}

func (in *Inode) SetFlags(flags uint32) {
	in.flags = flags
}

func (in *Inode) Pgid() Pgid {
	return in.pgid
}

func (in *Inode) SetPgid(id Pgid) {
	in.pgid = id
}

func (in *Inode) Key() []byte {
	return in.key
}

func (in *Inode) SetKey(key []byte) {
	in.key = key
}

func (in *Inode) Value() []byte {
	return in.value
}

func (in *Inode) SetValue(value []byte) {
	in.value = value
}

func ReadInodeFromPage(p *Page) Inodes {
	inodes := make(Inodes, int(p.Count()))
	isLeaf := p.IsLeafPage()
	for i := 0; i < int(p.Count()); i++ {
		inode := &inodes[i]
		if isLeaf {
			elem := p.LeafPageElement(uint16(i))
			inode.SetFlags(elem.Flags())
			inode.SetKey(elem.Key())
			inode.SetValue(elem.Value())
		} else {
			elem := p.BranchPageElement(uint16(i))
			inode.SetPgid(elem.Pgid())
			inode.SetKey(elem.Key())
		}
		Assert(len(inode.Key()) > 0, "read: zero-length inode key")
	}

	return inodes
}

func WriteInodeToPage(inodes Inodes, p *Page) uint32 {
	// Loop over each item and write it to the page.
	// off tracks the offset into p of the start of the next data.
	off := unsafe.Sizeof(*p) + p.PageElementSize()*uintptr(len(inodes))
	isLeaf := p.IsLeafPage()
	for i, item := range inodes {
		Assert(len(item.Key()) > 0, "write: zero-length inode key")

		// Create a slice to write into of needed size and advance
		// byte pointer for next iteration.
		sz := len(item.Key()) + len(item.Value())
		b := UnsafeByteSlice(unsafe.Pointer(p), off, 0, sz)
		off += uintptr(sz)

		// Write the page element.
		if isLeaf {
			elem := p.LeafPageElement(uint16(i))
			elem.SetPos(uint32(uintptr(unsafe.Pointer(&b[0])) - uintptr(unsafe.Pointer(elem))))
			elem.SetFlags(item.Flags())
			elem.SetKsize(uint32(len(item.Key())))
			elem.SetVsize(uint32(len(item.Value())))
		} else {
			elem := p.BranchPageElement(uint16(i))
			elem.SetPos(uint32(uintptr(unsafe.Pointer(&b[0])) - uintptr(unsafe.Pointer(elem))))
			elem.SetKsize(uint32(len(item.Key())))
			elem.SetPgid(item.Pgid())
			Assert(elem.Pgid() != p.Id(), "write: circular dependency occurred")
		}

		// Write data for the element to the end of the page.
		l := copy(b, item.Key())
		copy(b[l:], item.Value())
	}

	return uint32(off)
}

func UsedSpaceInPage(inodes Inodes, p *Page) uint32 {
	off := unsafe.Sizeof(*p) + p.PageElementSize()*uintptr(len(inodes))
	for _, item := range inodes {
		sz := len(item.Key()) + len(item.Value())
		off += uintptr(sz)
	}

	return uint32(off)
}
