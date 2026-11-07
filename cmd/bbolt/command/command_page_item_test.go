package command_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	bolt "go.etcd.io/bbolt"
	"go.etcd.io/bbolt/cmd/bbolt/command"
	"go.etcd.io/bbolt/internal/btesting"
	"go.etcd.io/bbolt/internal/guts_cli"
)

func TestPageItemCommand_Run(t *testing.T) {
	testCases := []struct {
		name          string
		printable     bool
		itemId        string
		expectedKey   string
		expectedValue string
	}{
		{
			name:          "printable items",
			printable:     true,
			itemId:        "0",
			expectedKey:   "key_0",
			expectedValue: "value_0",
		},
		{
			name:          "non printable items",
			printable:     false,
			itemId:        "0",
			expectedKey:   hex.EncodeToString(convertInt64IntoBytes(0 + 1)),
			expectedValue: hex.EncodeToString(convertInt64IntoBytes(0 + 2)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := btesting.MustCreateDBWithOption(t, &bolt.Options{PageSize: 4096})
			srcPath := db.Path()

			t.Log("Inserting some sample data")
			err := db.Update(func(tx *bolt.Tx) error {
				b, bErr := tx.CreateBucketIfNotExists([]byte("data"))
				if bErr != nil {
					return bErr
				}

				for i := 0; i < 100; i++ {
					if tc.printable {
						if bErr = b.Put([]byte(fmt.Sprintf("key_%d", i)), []byte(fmt.Sprintf("value_%d", i))); bErr != nil {
							return bErr
						}
					} else {
						k, v := convertInt64IntoBytes(int64(i+1)), convertInt64IntoBytes(int64(i+2))
						if bErr = b.Put(k, v); bErr != nil {
							return bErr
						}
					}
				}
				return nil
			})
			require.NoError(t, err)
			require.NoError(t, db.Close())
			defer requireDBNoChange(t, dbData(t, srcPath), srcPath)

			meta := readMetaPage(t, srcPath)
			leafPageId := 0
			for i := 2; i < int(meta.Pgid()); i++ {
				p, _, err := guts_cli.ReadPage(srcPath, uint64(i))
				require.NoError(t, err)
				if p.IsLeafPage() && p.Count() > 1 {
					leafPageId = int(p.Id())
				}
			}
			require.NotEqual(t, 0, leafPageId)

			t.Log("Running page-item command")
			rootCmd := command.NewRootCommand()
			outBuf := &bytes.Buffer{}
			rootCmd.SetOut(outBuf)
			rootCmd.SetArgs([]string{"page-item", db.Path(), fmt.Sprintf("%d", leafPageId), tc.itemId})
			err = rootCmd.Execute()
			require.NoError(t, err)

			t.Log("Checking output")
			output := outBuf.String()
			require.True(t, strings.Contains(output, tc.expectedKey), "unexpected output:", output)
			require.True(t, strings.Contains(output, tc.expectedValue), "unexpected output:", output)
		})
	}
}

func TestPageItemCommand_NoArgs(t *testing.T) {
	expErr := errors.New("accepts 3 arg(s), received 0")
	rootCmd := command.NewRootCommand()
	rootCmd.SetArgs([]string{"page-item"})
	err := rootCmd.Execute()
	require.ErrorContains(t, err, expErr.Error())
}

func TestPageItemCommand_ItemIDOutOfRange(t *testing.T) {
	db := btesting.MustCreateDBWithOption(t, &bolt.Options{PageSize: 4096})
	srcPath := db.Path()

	err := db.Update(func(tx *bolt.Tx) error {
		b, bErr := tx.CreateBucketIfNotExists([]byte("data"))
		if bErr != nil {
			return bErr
		}
		for i := 0; i < 100; i++ {
			if bErr = b.Put([]byte(fmt.Sprintf("key_%d", i)), []byte(fmt.Sprintf("value_%d", i))); bErr != nil {
				return bErr
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())
	defer requireDBNoChange(t, dbData(t, srcPath), srcPath)

	meta := readMetaPage(t, srcPath)
	leafPageId := 0
	for i := 2; i < int(meta.Pgid()); i++ {
		p, _, err := guts_cli.ReadPage(srcPath, uint64(i))
		require.NoError(t, err)
		if p.IsLeafPage() && p.Count() > 1 {
			leafPageId = int(p.Id())
		}
	}
	require.NotEqual(t, 0, leafPageId)

	testCases := []struct {
		name   string
		itemId string
	}{
		// Congruent to the valid index 0 modulo 2^16.
		{name: "wraps onto a valid index", itemId: "65536"},
		// Congruent to the out of range index 100 modulo 2^16, so the
		// id reported back to the user has to be the one that was typed.
		{name: "wraps onto an out of range index", itemId: "65636"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rootCmd := command.NewRootCommand()
			outBuf := &bytes.Buffer{}
			rootCmd.SetOut(outBuf)
			rootCmd.SetArgs([]string{"page-item", srcPath, fmt.Sprintf("%d", leafPageId), tc.itemId})
			err := rootCmd.Execute()
			require.ErrorContains(t, err, fmt.Sprintf("expected item index less than 100, but got %s", tc.itemId))
		})
	}
}
