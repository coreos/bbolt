package bbolt_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	bolt "go.etcd.io/bbolt"
	"go.etcd.io/bbolt/internal/common"
	"go.etcd.io/bbolt/internal/guts_cli"
)

// A branch page whose child points back at itself must not send a cursor
// down forever. Before the depth bound, ForEach grew the stack until the
// process ran out of memory.
func TestCursorPanicsOnABranchCycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cycle.db")
	db, err := bolt.Open(path, 0600, nil)
	require.NoError(t, err)
	var root common.Pgid
	require.NoError(t, db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucket([]byte("b"))
		if err != nil {
			return err
		}
		for i := 0; i < 2000; i++ {
			if err := b.Put([]byte(fmt.Sprintf("key-%06d", i)), make([]byte, 64)); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, db.View(func(tx *bolt.Tx) error {
		root = tx.Bucket([]byte("b")).RootPage()
		return nil
	}))
	require.NoError(t, db.Close())

	p, buf, err := guts_cli.ReadPage(path, uint64(root))
	require.NoError(t, err)
	require.True(t, p.IsBranchPage(), "the bucket needs a branch root for this test")
	common.LoadPage(buf).BranchPageElement(0).SetPgid(root)
	require.NoError(t, guts_cli.WritePage(path, buf))

	db, err = bolt.Open(path, 0600, nil)
	require.NoError(t, err)
	defer db.Close()

	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		_ = db.View(func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("b")).ForEach(func(_, _ []byte) error { return nil })
		})
	}()
	select {
	case r := <-done:
		require.NotNil(t, r)
		require.True(t, strings.Contains(fmt.Sprint(r), "cycles back"), "panic: %v", r)
	case <-time.After(10 * time.Second):
		t.Fatal("ForEach did not stop on the cycle")
	}
}
