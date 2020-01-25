package bbolt_test

import (
	"bytes"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// Ensure the usercan register their sort functions
func Test_RegisterSort(t *testing.T) {
	mysort := func(a, b []byte) int {
		return 0
	}
	if err := bolt.RegisterSort(mysort); err != nil {
		t.Fatal(err)
	}
}

// Ensure that registering can't accidentally override a function with the same hash
func Test_RegisterSort_Duplicate(t *testing.T) {
	mysort := func(a, b []byte) int {
		return 0
	}
	if err := bolt.RegisterSort(mysort); err != nil {
		t.Fatal(err)
	}
	if err := bolt.RegisterSort(mysort); err == nil {
		t.Fail()
	}
}

// Ensure the user can't register the default sort (bytes.Compare)
func Test_RegisterSort_Bytes(t *testing.T) {
	if err := bolt.RegisterSort(bytes.Compare); err == nil {
		t.Fail()
	}
}
