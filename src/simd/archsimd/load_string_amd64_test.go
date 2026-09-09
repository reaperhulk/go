// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.simd && amd64

package archsimd_test

import (
	"simd/archsimd"
	"strings"
	"testing"
)

func TestLoadStringDoesNotAllocate(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 required")
	}
	s := strings.Repeat("x", 1024)
	var out [32]byte
	allocs := testing.AllocsPerRun(100, func() {
		v := archsimd.LoadUint8x32([]byte(s))
		v.StoreArray(&out)
		archsimd.ClearAVXUpperBits()
	})
	if allocs != 0 {
		t.Fatalf("got %v allocations per load, want 0", allocs)
	}
	if string(out[:]) != s[:32] {
		t.Fatalf("loaded %q, want %q", out, s[:32])
	}
}

func TestStoreStringStillCopies(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 required")
	}
	s := strings.Repeat("x", 1024)
	b := []byte(s)
	archsimd.BroadcastUint8x32('y').StoreArray((*[32]byte)(b))
	archsimd.ClearAVXUpperBits()
	if s != strings.Repeat("x", 1024) {
		t.Fatal("SIMD store modified the original string")
	}
	if string(b[:32]) != strings.Repeat("y", 32) {
		t.Fatal("SIMD store did not modify the byte slice")
	}
}
