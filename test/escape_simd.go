// errorcheck -0 -m -d=escapemutationscalls,zerocopy -l

//go:build goexperiment.simd && (amd64 || arm64)

// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test that the SIMD load intrinsics are seen to only read through their
// pointer, so that memory passed to them is not treated as mutated, and
// that the store intrinsics are seen to write through theirs.

package p

import "simd/archsimd"

func load(s string) archsimd.Uint8x16 { // ERROR "s does not escape, mutate, or call"
	return archsimd.LoadUint8x16([]byte(s)) // ERROR "zero-copy string->\[\]byte conversion" "\(\[\]byte\)\(s\) does not escape"
}

func loadArray(s string) archsimd.Uint8x16 { // ERROR "s does not escape, mutate, or call"
	b := []byte(s) // ERROR "zero-copy string->\[\]byte conversion" "\(\[\]byte\)\(s\) does not escape"
	return archsimd.LoadUint8x16Array((*[16]uint8)(b))
}

func store(x archsimd.Uint8x16, p *[16]uint8) { // ERROR "mutates param: p derefs=0" "calls param: p derefs=0"
	x.StoreArray(p)
}
