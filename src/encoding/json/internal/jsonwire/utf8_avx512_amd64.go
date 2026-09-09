// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && goexperiment.simd && amd64

package jsonwire

import (
	"math/bits"
	"simd/archsimd"
)

func consumeUTF8PrefixAVX512(b []byte) (n int) {
	c0 := archsimd.BroadcastUint8x64(0xc0)
	e0 := archsimd.BroadcastUint8x64(0xe0)
	f0 := archsimd.BroadcastUint8x64(0xf0)
	fe := archsimd.BroadcastUint8x64(0xfe)
	x80 := archsimd.BroadcastUint8x64(0x80)
	zero := archsimd.BroadcastUint8x64(0)
	for len(b)-n >= 64 {
		v := archsimd.LoadUint8x64(b[n:])
		high := v.And(f0)
		lead2 := uint64(v.And(e0).Equal(c0).ToBits())
		lead3 := uint64(high.Equal(e0).ToBits())
		lead4 := uint64(high.Equal(f0).ToBits())
		cont := uint64(v.And(c0).Equal(x80).ToBits())

		// JSON-sensitive ASCII and the two JavaScript line separators end
		// the prefix. The latter mask marks the start of the three-byte rune.
		stop := v.And(e0).Equal(zero).
			Or(v.Equal(archsimd.BroadcastUint8x64('"'))).
			Or(v.Equal(archsimd.BroadcastUint8x64('\\'))).
			Or(v.Equal(archsimd.BroadcastUint8x64('&'))).
			Or(v.Or(archsimd.BroadcastUint8x64(2)).Equal(archsimd.BroadcastUint8x64('>'))).ToBits()
		e2 := v.Equal(archsimd.BroadcastUint8x64(0xe2)).ToBits()
		b80 := v.Equal(x80).ToBits()
		a8 := v.And(fe).Equal(archsimd.BroadcastUint8x64(0xa8)).ToBits()
		stop |= ((e2 << 2) & (b80 << 1) & a8) >> 2
		end := bits.TrailingZeros64(stop)
		mask := uint64(1)<<end - 1
		lead2 &= mask
		lead3 &= mask
		lead4 &= mask

		// Every lead requires exactly one, two, or three following
		// continuation bytes. Incomplete leads at the end are checked below.
		must := (lead2|lead3|lead4)<<1 | (lead3|lead4)<<2 | lead4<<3
		invalid := uint64(v.And(fe).Equal(c0).
			Or(v.Greater(archsimd.BroadcastUint8x64(0xf4))).ToBits())
		lowCont := uint64(v.And(e0).Equal(x80).ToBits()) // 80..9f
		high80 := uint64(high.Equal(x80).ToBits())       // 80..8f
		e0s := uint64(v.Equal(e0).ToBits())
		eds := uint64(v.Equal(archsimd.BroadcastUint8x64(0xed)).ToBits())
		f0s := uint64(v.Equal(f0).ToBits())
		f4s := uint64(v.Equal(archsimd.BroadcastUint8x64(0xf4)).ToBits())
		// Reject overlong encodings, surrogate code points, and > U+10FFFF.
		invalid |= (e0s << 1 & lowCont) | (eds << 1 &^ lowCont) |
			(f0s << 1 & high80) | (f4s << 1 &^ high80)
		if ((must^cont)|invalid)&mask != 0 {
			goto done
		}
		if must>>end != 0 || (end == 64 && (lead2>>63|lead3>>62|lead4>>61) != 0) {
			// Revisit a rune straddling the boundary in the next block (or
			// in the scalar caller); never return inside a UTF-8 sequence.
			end = bits.Len64(lead2|lead3|lead4) - 1
		}
		n += end
		if stop != 0 {
			goto done
		}
	}
	archsimd.ClearAVXUpperBits()
	if len(b)-n >= 32 {
		n += consumeUTF8PrefixSIMD(b[n:])
	}
	return n
done:
	archsimd.ClearAVXUpperBits()
	return n
}
