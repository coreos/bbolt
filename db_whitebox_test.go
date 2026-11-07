package bbolt

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.etcd.io/bbolt/errors"
)

func TestOpenWithPreLoadFreelist(t *testing.T) {
	testCases := []struct {
		name                    string
		readonly                bool
		preLoadFreePage         bool
		expectedFreePagesLoaded bool
	}{
		{
			name:                    "write mode always load free pages",
			readonly:                false,
			preLoadFreePage:         false,
			expectedFreePagesLoaded: true,
		},
		{
			name:                    "readonly mode load free pages when flag set",
			readonly:                true,
			preLoadFreePage:         true,
			expectedFreePagesLoaded: true,
		},
		{
			name:                    "readonly mode doesn't load free pages when flag not set",
			readonly:                true,
			preLoadFreePage:         false,
			expectedFreePagesLoaded: false,
		},
	}

	fileName, err := prepareData(t)
	require.NoError(t, err)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open(fileName, 0666, &Options{
				ReadOnly:        tc.readonly,
				PreLoadFreelist: tc.preLoadFreePage,
			})
			require.NoError(t, err)

			assert.Equal(t, tc.expectedFreePagesLoaded, db.freelist != nil)

			assert.NoError(t, db.Close())
		})
	}
}

func TestMethodPage(t *testing.T) {
	testCases := []struct {
		name            string
		readonly        bool
		preLoadFreePage bool
		expectedError   error
	}{
		{
			name:            "write mode",
			readonly:        false,
			preLoadFreePage: false,
			expectedError:   nil,
		},
		{
			name:            "readonly mode with preloading free pages",
			readonly:        true,
			preLoadFreePage: true,
			expectedError:   nil,
		},
		{
			name:            "readonly mode without preloading free pages",
			readonly:        true,
			preLoadFreePage: false,
			expectedError:   errors.ErrFreePagesNotLoaded,
		},
	}

	fileName, err := prepareData(t)
	require.NoError(t, err)

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open(fileName, 0666, &Options{
				ReadOnly:        tc.readonly,
				PreLoadFreelist: tc.preLoadFreePage,
			})
			require.NoError(t, err)
			defer db.Close()

			tx, err := db.Begin(!tc.readonly)
			require.NoError(t, err)

			_, err = tx.Page(0)
			require.Equal(t, tc.expectedError, err)

			if tc.readonly {
				require.NoError(t, tx.Rollback())
			} else {
				require.NoError(t, tx.Commit())
			}

			require.NoError(t, db.Close())
		})
	}
}

// TestTx_Commit_MetaWriteError_KeepsReaderSnapshot tests that an open read
// transaction keeps its snapshot when a Commit fails after its meta page was
// written.
func TestTx_Commit_MetaWriteError_KeepsReaderSnapshot(t *testing.T) {
	// A large initial mmap size, so that no remap waits for the open reader.
	db, err := Open(filepath.Join(t.TempDir(), "db"), 0600, &Options{InitialMmapSize: 1 << 20})
	require.NoError(t, err)
	defer db.Close()

	put := func(v string) error {
		return db.Update(func(tx *Tx) error {
			b, err := tx.CreateBucketIfNotExists([]byte("b"))
			if err != nil {
				return err
			}
			return b.Put([]byte("k"), []byte(v))
		})
	}
	require.NoError(t, put("v1"))

	rtx, err := db.Begin(false)
	require.NoError(t, err)
	defer func() { _ = rtx.Rollback() }()
	get := func() string { return string(rtx.Bucket([]byte("b")).Get([]byte("k"))) }
	require.Equal(t, "v1", get())

	// The meta page reaches the file, then its write reports an error, as
	// when fdatasync fails.
	writeAt := db.ops.writeAt
	db.ops.writeAt = func(b []byte, off int64) (int, error) {
		n, err := writeAt(b, off)
		if err == nil && off < int64(2*db.pageSize) {
			err = fmt.Errorf("write error after the meta page was written")
		}
		return n, err
	}
	require.Error(t, put("v2"))
	db.ops.writeAt = writeAt

	require.NoError(t, put("v3"))

	// The read transaction already read v1, so it must read v1 again.
	require.Equal(t, "v1", get())
}

func prepareData(t *testing.T) (string, error) {
	fileName := filepath.Join(t.TempDir(), "db")
	db, err := Open(fileName, 0666, nil)
	if err != nil {
		return "", err
	}
	if err := db.Close(); err != nil {
		return "", err
	}

	return fileName, nil
}
