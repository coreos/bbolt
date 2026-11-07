package bbolt_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
	"go.etcd.io/bbolt/internal/btesting"
)

// A CopyFile destination must never be allowed to refer to the live database
// inode. O_TRUNC on the source (including a symlink or hard link to it) would
// irreversibly discard the database before the copy even starts.
func TestTx_CopyFileRejectsSourceAliases(t *testing.T) {
	db := btesting.MustCreateDB(t)
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte("important"))
		if err != nil {
			return err
		}
		return bucket.Put([]byte("key"), []byte("durable-value"))
	}); err != nil {
		t.Fatal(err)
	}

	sourceInfo, err := os.Stat(db.Path())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{name: "source path", setup: func(t *testing.T) string {
			return db.Path()
		}},
		{name: "hardlink alias", setup: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "hardlink.db")
			if err := os.Link(db.Path(), path); err != nil {
				t.Skipf("hard links unavailable: %v", err)
			}
			return path
		}},
		{name: "symlink alias", setup: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "symlink.db")
			if err := os.Symlink(db.Path(), path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return path
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)
			if err := db.View(func(tx *bolt.Tx) error {
				return tx.CopyFile(path, 0600)
			}); err == nil {
				t.Fatal("expected CopyFile to reject source-file alias")
			}

			// The database must remain byte-preserved and readable.
			info, err := os.Stat(db.Path())
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != sourceInfo.Size() {
				t.Fatalf("source size changed from %d to %d", sourceInfo.Size(), info.Size())
			}
			if err := db.View(func(tx *bolt.Tx) error {
				if got := tx.Bucket([]byte("important")).Get([]byte("key")); !bytes.Equal(got, []byte("durable-value")) {
					t.Fatalf("source data changed: %q", got)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Existing destination files must still be overwritten and truncated to the
// exact copied database size: the new inode guard must not break backups.
func TestTx_CopyFileOverwritesUnrelatedDestination(t *testing.T) {
	db := btesting.MustCreateDB(t)
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte("important"))
		if err != nil {
			return err
		}
		return bucket.Put([]byte("key"), []byte("durable-value"))
	}); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "existing.db")
	source, err := os.Stat(db.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, bytes.Repeat([]byte("x"), int(source.Size())+4096), 0600); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *bolt.Tx) error {
		return tx.CopyFile(dst, 0600)
	}); err != nil {
		t.Fatal(err)
	}
	gotInfo, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	// CopyFile produces the logical transaction size, not the source file's
	// preallocated file size, which can legitimately be larger.
	var logicalSize int64
	if err := db.View(func(tx *bolt.Tx) error {
		logicalSize = tx.Size()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if gotInfo.Size() != logicalSize {
		t.Fatalf("backup size = %d, want exact logical size %d", gotInfo.Size(), logicalSize)
	}
	copy, err := bolt.Open(dst, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	if err := copy.View(func(tx *bolt.Tx) error {
		if got := tx.Bucket([]byte("important")).Get([]byte("key")); !bytes.Equal(got, []byte("durable-value")) {
			t.Fatalf("wrong backup value: %q", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
