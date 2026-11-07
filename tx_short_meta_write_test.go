package bbolt_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	bolt "go.etcd.io/bbolt"
	"go.etcd.io/bbolt/internal/btesting"
)

// A Writer that returns a short successful write exactly once. io.Writer
// implementations should report ErrShortWrite themselves, but WriteTo promises
// to report an error unless it wrote exactly tx.Size() bytes.
type shortMetaPageWriter struct {
	buf       bytes.Buffer
	shortCall int
	calls     int
}

func (w *shortMetaPageWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.shortCall {
		return w.buf.Write(p[:len(p)/2])
	}
	return w.buf.Write(p)
}

func TestTx_WriteToRejectsShortMetaWrites(t *testing.T) {
	for _, tc := range []struct {
		name      string
		shortCall int
	}{
		{name: "first meta page", shortCall: 1},
		{name: "second meta page", shortCall: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := btesting.MustCreateDB(t)
			if err := db.Update(func(tx *bolt.Tx) error {
				b, err := tx.CreateBucket([]byte("important"))
				if err != nil {
					return err
				}
				return b.Put([]byte("key"), []byte("durable"))
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.View(func(tx *bolt.Tx) error {
				w := &shortMetaPageWriter{shortCall: tc.shortCall}
				n, err := tx.WriteTo(w)
				if !errors.Is(err, io.ErrShortWrite) {
					t.Fatalf("WriteTo with short meta write (%d) returned (%d, %v), want io.ErrShortWrite", tc.shortCall, n, err)
				}
				if n != int64(w.buf.Len()) {
					t.Fatalf("reported bytes %d != actual bytes %d", n, w.buf.Len())
				}
				if n >= tx.Size() {
					t.Fatalf("short write reported full transaction length: %d >= %d", n, tx.Size())
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTx_WriteToFullWriterStillSucceeds(t *testing.T) {
	db := btesting.MustCreateDB(t)
	if err := db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucket([]byte("important"))
		if err != nil {
			return err
		}
		return b.Put([]byte("key"), []byte("durable"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *bolt.Tx) error {
		var w bytes.Buffer
		n, err := tx.WriteTo(&w)
		if err != nil {
			return err
		}
		if n != tx.Size() || int64(w.Len()) != tx.Size() {
			t.Fatalf("copied %d bytes, expected tx size %d", n, tx.Size())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
