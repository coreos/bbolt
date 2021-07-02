package bbolt

import (
	"time"
)

// maxMapSize represents the largest mmap size supported by Bolt.
const maxMapSize = 0x7FFFFFFF // 2GB

// maxAllocSize is the size used when creating array pointers.
const maxAllocSize = 0xFFFFFFF

// Are unaligned load/stores broken on this arch?
var brokenUnaligned bool

// flock acquires an advisory lock on a file descriptor.
func flock(db *DB, exclusive bool, timeout time.Duration) error {
	return nil
}

// funlock releases an advisory lock on a file descriptor.
func funlock(db *DB) error {
	// return syscall.Flock(int(db.file.Fd()), syscall.LOCK_UN)
	return nil
}

// mmap memory maps a DB's data file.
func mmap(db *DB, sz int) error {
	return nil
}

// munmap unmaps a DB's data file from memory.
func munmap(db *DB) error {
	return nil
}
