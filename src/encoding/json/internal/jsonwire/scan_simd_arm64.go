// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && goexperiment.simd && arm64

package jsonwire

import (
	"math/bits"
	"simd/archsimd"
)

// simdEnabled reports whether this build contains a vectorized scanner at all.
const simdEnabled = true

// simdHardware reports whether the CPU can run the vectorized scanners.
// NEON is part of the base arm64 architecture.
var simdHardware = true

// useSIMD reports whether the vectorized scanners may be used. It starts out
// equal to simdHardware, and is also flipped by benchmarks that want to measure
// the scalar and vector implementations against each other in one process.
var useSIMD = simdHardware

// neonBytes is the number of input bytes classified per loop iteration.
// NEON vectors are 16 bytes; vectorBytes, which the callers use as the
// minimum worth handing over, stays at 32 so two iterations are always run.
const neonBytes = 16

// classTablesNEON holds a scanTables pair loaded into vector registers.
// Only the first 16 entries of each table matter here: NEON's TBL looks up
// a full 16-byte table, so the tables need no duplication.
type classTablesNEON struct {
	lo, hi archsimd.Uint8x16
}

func loadClassTablesNEON(t *scanTables) classTablesNEON {
	return classTablesNEON{
		lo: archsimd.LoadUint8x16Array((*[16]uint8)(t.lo[:16])),
		hi: archsimd.LoadUint8x16Array((*[16]uint8)(t.hi[:16])),
	}
}

// members returns the bytes of v that belong to the class as 0xff and the
// rest as 0x00. This is the same nibble classifier as on amd64, with TBL in
// place of VPSHUFB; NEON has a byte shift, so the high nibbles need no
// 16-bit detour.
func (t *classTablesNEON) members(v archsimd.Uint8x16) archsimd.Uint8x16 {
	nibble := archsimd.BroadcastUint8x16(0x0f)
	class := t.lo.LookupOrZero(v.And(nibble)).And(t.hi.LookupOrZero(v.ShiftAllRight(4)))
	var zero archsimd.Uint8x16
	return archsimd.BroadcastUint8x16(0xff).Masked(class.NotEqual(zero))
}

// firstSet returns the index of the first 0xff byte of m, or -1 if there is
// none. NEON has no move-mask instruction; extracting the two 64-bit halves
// and counting trailing zeros is the cheapest equivalent for a 16-byte vector
// when only the first set byte is wanted.
func firstSet(m archsimd.Uint8x16) int {
	halves := m.ReshapeToUint64s()
	if lo := halves.GetElem(0); lo != 0 {
		return bits.TrailingZeros64(lo) / 8
	}
	if hi := halves.GetElem(1); hi != 0 {
		return 8 + bits.TrailingZeros64(hi)/8
	}
	return -1
}

// indexClassSIMD returns the index of the first byte of b that belongs to the
// character class described by t, considering only whole neonBytes-sized
// blocks. If no such byte is found it reports the number of bytes it examined
// and false.
func indexClassSIMD(t *scanTables, b []byte) (int, bool) {
	if len(b) < vectorBytes {
		return 0, false
	}
	tables := loadClassTablesNEON(t)
	var i int
	for ; i+neonBytes <= len(b); i += neonBytes {
		if k := firstSet(tables.members(archsimd.LoadUint8x16(b[i:]))); k >= 0 {
			return i + k, true
		}
	}
	return i, false
}

// indexStringByteLong is the out-of-line half of [indexStringByte].
func indexStringByteLong(b []byte) int {
	var i int
	if useSIMD {
		n, found := indexClassSIMD(&stringTables, b)
		if found {
			return n
		}
		i = n
	}
	for ; uint(len(b)) > uint(i); i++ {
		if stringByteTable[b[i]] != 0 {
			return i
		}
	}
	return len(b)
}

// indexEscapeByteLong is the out-of-line half of [indexEscapeByte].
func indexEscapeByteLong(b []byte) int {
	var i int
	if useSIMD {
		n, found := indexClassSIMD(&escapeTables, b)
		if found {
			return n
		}
		i = n
	}
	for ; uint(len(b)) > uint(i); i++ {
		if escapeByteTable[b[i]] != 0 {
			return i
		}
	}
	return len(b)
}

// consumeWhitespaceLong is the out-of-line half of [ConsumeWhitespace].
func consumeWhitespaceLong(b []byte) int {
	if len(b) > 1 && b[0] == ' ' && b[1] > ' ' {
		return 1 // the space after a colon; see consumeWhitespaceScalar
	}
	var n int
	if useSIMD && len(b) >= vectorBytes {
		tables := loadClassTablesNEON(&whitespaceTables)
		for ; n+neonBytes <= len(b); n += neonBytes {
			// The class is whitespace, so look for the first byte not in it.
			if k := firstSet(tables.members(archsimd.LoadUint8x16(b[n:])).Not()); k >= 0 {
				return n + k
			}
		}
	}
	return n + consumeWhitespaceScalar(b[n:])
}
