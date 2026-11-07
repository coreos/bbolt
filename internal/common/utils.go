package common

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unsafe"
)

// LoadBucket decodes an on-disk bucket header by copy.
// The value of a leaf element starts at pos+ksize and is not 8-byte aligned
// when the key length is odd. Casting &buf[0] to *InBucket is rejected by
// checkptr and is SIGBUS on architectures that fault unaligned 64-bit loads.
// A short buffer returns nil. Callers must nil-check. The returned pointer is
// not a writable view of the page.
func LoadBucket(buf []byte) *InBucket {
	if len(buf) < BucketHeaderSize {
		return nil
	}
	b := NewInBucket(
		Pgid(binary.NativeEndian.Uint64(buf[0:8])),
		binary.NativeEndian.Uint64(buf[8:16]),
	)
	return &b
}

func LoadPage(buf []byte) *Page {
	return (*Page)(unsafe.Pointer(&buf[0]))
}

func LoadPageMeta(buf []byte) *Meta {
	return (*Meta)(unsafe.Pointer(&buf[PageHeaderSize]))
}

func CopyFile(srcPath, dstPath string) error {
	// Ensure source file exists.
	_, err := os.Stat(srcPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("source file %q not found", srcPath)
	} else if err != nil {
		return err
	}

	// Ensure output file not exist.
	_, err = os.Stat(dstPath)
	if err == nil {
		return fmt.Errorf("output file %q already exists", dstPath)
	} else if !os.IsNotExist(err) {
		return err
	}

	srcDB, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %q: %w", srcPath, err)
	}
	defer srcDB.Close()
	dstDB, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create output file %q: %w", dstPath, err)
	}
	defer dstDB.Close()
	written, err := io.Copy(dstDB, srcDB)
	if err != nil {
		return fmt.Errorf("failed to copy database file from %q to %q: %w", srcPath, dstPath, err)
	}

	srcFi, err := srcDB.Stat()
	if err != nil {
		return fmt.Errorf("failed to get source file info %q: %w", srcPath, err)
	}
	initialSize := srcFi.Size()
	if initialSize != written {
		return fmt.Errorf("the byte copied (%q: %d) isn't equal to the initial db size (%q: %d)", dstPath, written, srcPath, initialSize)
	}

	return nil
}
