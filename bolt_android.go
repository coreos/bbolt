package bbolt

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"go.etcd.io/bbolt/errors"
	"go.etcd.io/bbolt/internal/common"
)

// Android is an ordinary Linux system, except on the FUSE mount that backs
// emulated external storage (/storage/emulated/...): MediaProvider's FUSE
// daemon advertises FUSE_CAP_FLOCK_LOCKS but leaves the flock handler
// unimplemented, so libfuse answers the request with ENOSYS and the kernel
// forwards that to userspace rather than falling back to local locking.
// flock(2) fails with ENOSYS there, while databases in the app's own data
// directory are unaffected.
// See https://github.com/etcd-io/bbolt/issues/558.
//
// Open file description locks work on that mount and carry the same ownership
// semantics as flock(2): the lock belongs to the open file description, so a
// second Open of the same file conflicts even from within one process, and
// closing another descriptor to the file leaves the lock in place.
//
// F_OFD_SETLK requires Linux 3.15, which this path always has: it is only
// reached on the FUSE mount, introduced in Android 11 with a 4.14 or newer
// kernel.

// flock acquires an advisory lock on a file descriptor.
func flock(db *DB, exclusive bool, timeout time.Duration) error {
	var t time.Time
	if timeout != 0 {
		t = time.Now()
	}
	fd := db.file.Fd()
	for {
		// Attempt to obtain an exclusive lock.
		err := lockFile(fd, exclusive)
		if err == nil {
			return nil
		} else if !isLockHeldByOther(err) {
			return err
		}

		// If we timed out then return an error.
		if timeout != 0 && time.Since(t) > timeout-flockRetryTimeout {
			return errors.ErrTimeout
		}

		// Wait for a bit and try again.
		time.Sleep(flockRetryTimeout)
	}
}

// funlock releases an advisory lock on a file descriptor.
func funlock(db *DB) error {
	fd := db.file.Fd()
	err := syscall.Flock(int(fd), syscall.LOCK_UN)
	if err != syscall.ENOSYS {
		return err
	}
	return ofdSetlk(fd, unix.F_UNLCK)
}

// lockFile takes a non-blocking lock on the whole file, using flock(2) where
// the filesystem implements it and an open file description lock elsewhere.
func lockFile(fd uintptr, exclusive bool) error {
	how := syscall.LOCK_SH
	lockType := int16(unix.F_RDLCK)
	if exclusive {
		how = syscall.LOCK_EX
		lockType = unix.F_WRLCK
	}

	err := syscall.Flock(int(fd), how|syscall.LOCK_NB)
	if err != syscall.ENOSYS {
		return err
	}
	return ofdSetlk(fd, lockType)
}

// ofdSetlk applies lockType to the whole file as an open file description lock.
func ofdSetlk(fd uintptr, lockType int16) error {
	lock := unix.Flock_t{
		Type:   lockType,
		Whence: 0, // SEEK_SET
		Start:  0,
		Len:    0, // to the end of the file
	}
	return unix.FcntlFlock(fd, unix.F_OFD_SETLK, &lock)
}

// isLockHeldByOther reports whether err means the lock is currently held
// elsewhere, so the attempt is worth retrying. flock(2) reports this as
// EWOULDBLOCK, which is EAGAIN on Linux; fcntl(2) may use either EAGAIN or
// EACCES.
func isLockHeldByOther(err error) bool {
	return err == syscall.EAGAIN || err == syscall.EACCES
}

// mmap memory maps a DB's data file.
func mmap(db *DB, sz int) error {
	// Map the data file to memory.
	b, err := unix.Mmap(int(db.file.Fd()), 0, sz, syscall.PROT_READ, syscall.MAP_SHARED|db.MmapFlags)
	if err != nil {
		return err
	}

	// Advise the kernel that the mmap is accessed randomly.
	err = unix.Madvise(b, syscall.MADV_RANDOM)
	if err != nil && err != syscall.ENOSYS {
		// Ignore not implemented error in kernel because it still works.
		return fmt.Errorf("madvise: %s", err)
	}

	// Save the original byte slice and convert to a byte array pointer.
	db.dataref = b
	db.data = (*[common.MaxMapSize]byte)(unsafe.Pointer(&b[0]))
	db.datasz = sz
	return nil
}

// munmap unmaps a DB's data file from memory.
func munmap(db *DB) error {
	// Ignore the unmap if we have no mapped data.
	if db.dataref == nil {
		return nil
	}

	// Unmap using the original byte slice.
	err := unix.Munmap(db.dataref)
	db.dataref = nil
	db.data = nil
	db.datasz = 0
	return err
}
