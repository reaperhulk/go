// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && goexperiment.simd && amd64

package jsonwire

import (
	"math/bits"
	"simd/archsimd"
)

func consumeASCIIPrefix(b []byte) int {
	if len(b) >= 32 && archsimd.X86.AVX2() {
		return consumeASCIIPrefixAVX2(b)
	}
	return 0
}

// consumeASCIIPrefixAVX2 skips ASCII bytes that need no escaping, including
// HTML escaping. It leaves any final partial vector to the caller.
func consumeASCIIPrefixAVX2(b []byte) (n int) {
	space := archsimd.BroadcastInt8x32(32)
	quote := archsimd.BroadcastUint8x32('"')
	slash := archsimd.BroadcastUint8x32('\\')
	amp := archsimd.BroadcastUint8x32('&')
	two := archsimd.BroadcastUint8x32(2)
	gt := archsimd.BroadcastUint8x32('>')
	for len(b)-n >= 32 {
		v := archsimd.LoadUint8x32(b[n:])
		// The signed comparison also rejects non-ASCII bytes. OR with 2
		// folds both HTML-sensitive angle brackets to the same byte.
		bad := space.Greater(v.BitsToInt8()).Or(v.Equal(quote)).Or(v.Equal(slash)).Or(v.Equal(amp)).Or(v.Or(two).Equal(gt)).ToBits()
		if bad != 0 {
			n += bits.TrailingZeros32(bad)
			break
		}
		n += 32
	}
	archsimd.ClearAVXUpperBits()
	return n
}
