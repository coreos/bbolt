//go:build js

package bbolt

// js/wasm support. The browser (and other js runtimes) have no flock and no
// mmap: file locks are no-ops — a wasm instance is single-process by
// construction — and "mmap" reads the mapped region through the runtime's
// file API (globalThis.fs under Go's wasm_exec.js) into an ordinary byte
// slice, which fdatasync re-reads after each commit (so NoSync is ignored
// on js). Whether opening a database works at runtime
// depends on the filesystem the host environment provides (Node passes
// through to the real filesystem; a browser page must install its own
// globalThis.fs implementation).

import (
	"io"
	"time"
	"unsafe"

	"go.etcd.io/bbolt/internal/common"
)

// flock acquires an advisory lock on a file descriptor. No-op on js.
func flock(_ *DB, _ bool, _ time.Duration) error { return nil }

// funlock releases an advisory lock on a file descriptor. No-op on js.
func funlock(_ *DB) error { return nil }

// fdatasync flushes written data to a file descriptor and refreshes the
// emulated mapping: a real mmap observes committed pages as soon as they
// reach the file, but this mapping is a copy taken at map time. Re-reading
// at the commit flush point keeps page reads coherent with the write.
func fdatasync(db *DB) error {
	if err := db.file.Sync(); err != nil {
		return err
	}
	if db.dataref != nil {
		if n, err := db.file.ReadAt(db.dataref, 0); err != nil && err != io.EOF && n == 0 {
			return err
		}
	}
	return nil
}

// mmap emulates memory-mapping by reading the file region into a slice.
func mmap(db *DB, sz int) error {
	b := make([]byte, sz)
	n, err := db.file.ReadAt(b, 0)
	// A short read is expected: bbolt maps beyond the current end of the
	// file. Only a real error (with nothing read past it) is fatal.
	if err != nil && err != io.EOF && n == 0 {
		return err
	}
	db.dataref = b
	db.data = (*[common.MaxMapSize]byte)(unsafe.Pointer(&b[0]))
	db.datasz = sz
	return nil
}

// munmap releases the emulated mapping.
func munmap(db *DB) error {
	db.dataref = nil
	db.data = nil
	db.datasz = 0
	return nil
}
