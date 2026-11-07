package common

// GOARCH=wasm: linear memory is a 32-bit-style address space; mirror the 386 bounds.

// MaxMapSize represents the largest mmap size supported by Bolt.
const MaxMapSize = 0x7FFFFFFF // 2GB

// MaxAllocSize is the size used when creating array pointers.
const MaxAllocSize = 0xFFFFFFF
