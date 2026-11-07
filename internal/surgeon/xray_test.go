package surgeon_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.etcd.io/bbolt"
	"go.etcd.io/bbolt/internal/btesting"
	"go.etcd.io/bbolt/internal/common"
	"go.etcd.io/bbolt/internal/guts_cli"
	"go.etcd.io/bbolt/internal/surgeon"
)

func TestFindPathsToKey(t *testing.T) {
	db := btesting.MustCreateDB(t)
	assert.NoError(t,
		db.Fill([]byte("data"), 1, 500,
			func(tx int, k int) []byte { return []byte(fmt.Sprintf("%04d", k)) },
			func(tx int, k int) []byte { return make([]byte, 100) },
		))
	assert.NoError(t, db.Close())

	navigator := surgeon.NewXRay(db.Path())
	path1, err := navigator.FindPathsToKey([]byte("0451"))
	assert.NoError(t, err)
	assert.NotEmpty(t, path1)

	page := path1[0][len(path1[0])-1]
	p, _, err := guts_cli.ReadPage(db.Path(), uint64(page))
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, []byte("0451"), p.LeafPageElement(0).Key())
	assert.LessOrEqual(t, []byte("0451"), p.LeafPageElement(p.Count()-1).Key())
}

// TestFindPathsToKey_CycleDetected corrupts the db so that a branch page
// points at its own ancestor, then verifies that FindPathsToKey returns an
// error instead of recursing until stack overflow. See issue #701 for the
// real-world corruption pattern (power-off creating a page cycle).
func TestFindPathsToKey_CycleDetected(t *testing.T) {
	// A fixed page size keeps the tree two levels deep on hosts with larger
	// pages, such as js/wasm (64 KiB).
	db := btesting.MustCreateDBWithOption(t, &bbolt.Options{PageSize: 4096})
	require.NoError(t,
		db.Fill([]byte("data"), 1, 500,
			func(tx int, k int) []byte { return []byte(fmt.Sprintf("%04d", k)) },
			func(tx int, k int) []byte { return make([]byte, 100) },
		))
	require.NoError(t, db.Close())

	// Find the path to an arbitrary key so we can pick a branch ancestor
	// and one of its leaf descendants to corrupt.
	navigator := surgeon.NewXRay(db.Path())
	paths, err := navigator.FindPathsToKey([]byte("0001"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	path := paths[0]
	require.GreaterOrEqual(t, len(path), 2, "need at least one branch above the leaf")

	// Overwrite the leaf page with a copy of its branch ancestor. The
	// rewritten page is still a branch and now references its own pgid
	// through the ancestor's element list, forming a cycle.
	ancestor := path[len(path)-2]
	leaf := path[len(path)-1]
	require.NoError(t, surgeon.CopyPage(db.Path(), ancestor, leaf))

	// Confirm the ancestor actually lists the leaf as one of its children,
	// so copying creates a real cycle rather than two disjoint branches.
	ancestorPage, _, err := guts_cli.ReadPage(db.Path(), uint64(ancestor))
	require.NoError(t, err)
	require.True(t, ancestorPage.IsBranchPage())
	var hasLeafAsChild bool
	for i := uint16(0); i < ancestorPage.Count(); i++ {
		if ancestorPage.BranchPageElement(i).Pgid() == common.Pgid(leaf) {
			hasLeafAsChild = true
			break
		}
	}
	require.True(t, hasLeafAsChild, "expected ancestor to reference the leaf directly")

	_, err = surgeon.NewXRay(db.Path()).FindPathsToKey([]byte("0001"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle detected")
}

// TestFindPathsToKey_MultipleBuckets ensures the shared visited map in
// traverse does not suppress legitimate paths when the same key appears
// in more than one bucket. Each top-level bucket owns a disjoint page
// sub-tree, so both occurrences of the key must be returned.
func TestFindPathsToKey_MultipleBuckets(t *testing.T) {
	sharedKey := []byte("shared")
	bucketA := []byte("bucketA")
	bucketB := []byte("bucketB")

	db := btesting.MustCreateDB(t)
	// Fill each bucket with enough entries that it owns a real (non-inline)
	// root page and its own leaf pages, so the shared key lands on a pgid
	// the visited map actually tracks.
	require.NoError(t, db.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{bucketA, bucketB} {
			b, err := tx.CreateBucket(name)
			require.NoError(t, err)
			for i := 0; i < 500; i++ {
				k := []byte(fmt.Sprintf("%04d", i))
				require.NoError(t, b.Put(k, make([]byte, 100)))
			}
			require.NoError(t, b.Put(sharedKey, []byte("v")))
		}
		return nil
	}))
	require.NoError(t, db.Close())

	paths, err := surgeon.NewXRay(db.Path()).FindPathsToKey(sharedKey)
	require.NoError(t, err)
	require.Len(t, paths, 2, "expected one path per bucket containing the shared key")

	leafPgids := make(map[common.Pgid]struct{})
	for _, p := range paths {
		require.NotEmpty(t, p)
		leaf := p[len(p)-1]
		leafPgids[leaf] = struct{}{}

		page, _, err := guts_cli.ReadPage(db.Path(), uint64(leaf))
		require.NoError(t, err)
		require.True(t, page.IsLeafPage(), "terminal page in stack must be a leaf")
	}
	require.Len(t, leafPgids, 2, "the two paths must terminate on distinct leaf pages")
}

func TestFindPathsToKey_Bucket(t *testing.T) {
	rootBucket := []byte("data")
	subBucket := []byte("0451A")

	db := btesting.MustCreateDB(t)
	assert.NoError(t,
		db.Fill(rootBucket, 1, 500,
			func(tx int, k int) []byte { return []byte(fmt.Sprintf("%04d", k)) },
			func(tx int, k int) []byte { return make([]byte, 100) },
		))
	require.NoError(t, db.Update(func(tx *bbolt.Tx) error {
		sb, err := tx.Bucket(rootBucket).CreateBucket(subBucket)
		require.NoError(t, err)
		require.NoError(t, sb.Put([]byte("foo"), []byte("bar")))
		return nil
	}))

	assert.NoError(t, db.Close())

	navigator := surgeon.NewXRay(db.Path())
	path1, err := navigator.FindPathsToKey(subBucket)
	assert.NoError(t, err)
	assert.NotEmpty(t, path1)

	page := path1[0][len(path1[0])-1]
	p, _, err := guts_cli.ReadPage(db.Path(), uint64(page))
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, subBucket, p.LeafPageElement(0).Key())
	assert.LessOrEqual(t, subBucket, p.LeafPageElement(p.Count()-1).Key())
}
