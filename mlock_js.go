//go:build js

package bbolt

// mlock/munlock are unavailable on js/wasm; locking pages has no meaning in
// a wasm linear memory.
func mlock(_ *DB, _ int) error { return nil }

func munlock(_ *DB, _ int) error { return nil }
