// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && goexperiment.simd && amd64

package jsonwire

import (
	"math/bits"
	"simd/archsimd"
)

// simdEnabled reports whether this build contains a vectorized scanner at all.
// It is a constant so that builds without one pay nothing for the check.
const simdEnabled = true

// simdHardware reports whether the CPU can run the vectorized scanners.
var simdHardware = archsimd.X86.AVX2()

// useSIMD reports whether the vectorized scanners may be used. It starts out
// equal to simdHardware, and is also flipped by benchmarks that want to measure
// the scalar and vector implementations against each other in one process.
var useSIMD = simdHardware

// vectorBytes is the number of input bytes classified per loop iteration.
const vectorBytes = 32

// classTablesSIMD holds a scanTables pair loaded into vector registers.
type classTablesSIMD struct {
	lo, hi, nibble archsimd.Uint8x32
}

func loadClassTables(t *scanTables) classTablesSIMD {
	return classTablesSIMD{
		lo:     archsimd.LoadUint8x32Array(&t.lo),
		hi:     archsimd.LoadUint8x32Array(&t.hi),
		nibble: archsimd.BroadcastUint8x32(0x0f),
	}
}

// members returns a bitmask with bit i set if byte i of v belongs to the
// class described by t.
//
// This is the simdjson nibble trick described in scan.go: two VPSHUFB
// lookups, one indexed by the low nibble of each input byte and one by the
// high nibble, ANDed together. Bytes that are members of the class have a
// non-zero product, which VPCMPEQB and VPMOVMSKB turn into a bitmask.
func (t *classTablesSIMD) members(v archsimd.Uint8x32) uint32 {
	// AVX2 has no byte-granular shift, so the high nibbles are extracted
	// with a 16-bit shift followed by a mask.
	lo := v.And(t.nibble)
	hi := v.ReshapeToUint16s().ShiftAllRight(4).ReshapeToUint8s().And(t.nibble)
	class := t.lo.PermuteOrZeroGrouped(lo.AsInt8x32()).
		And(t.hi.PermuteOrZeroGrouped(hi.AsInt8x32()))
	// Equal(zero) marks the bytes that are *not* in the class, so the
	// complement of the bitmask marks the ones that are.
	var zero archsimd.Uint8x32
	return ^class.Equal(zero).ToBits()
}

// indexClassSIMD returns the index of the first byte of b that belongs to the
// character class described by t, considering only whole vectorBytes-sized
// blocks. If no such byte is found it reports the number of bytes it examined,
// which is the largest multiple of vectorBytes that fits in b, and false.
func indexClassSIMD(t *scanTables, b []byte) (int, bool) {
	if len(b) < vectorBytes {
		return 0, false
	}
	tables := loadClassTables(t)

	// Every return below first clears the upper halves of the YMM registers.
	// Leaving them dirty makes each legacy-SSE instruction that runs
	// afterwards, such as the small copies in memmove, pay a transition
	// penalty until something clears them, and the encoder copies right
	// after every scan. Without this, encoding measures up to 70% slower on
	// documents whose scans never even reach a vector.
	var i int
	for ; i+vectorBytes <= len(b); i += vectorBytes {
		if m := tables.members(archsimd.LoadUint8x32(b[i:])); m != 0 {
			archsimd.ClearAVXUpperBits()
			return i + bits.TrailingZeros32(m), true
		}
	}
	archsimd.ClearAVXUpperBits()
	return i, false
}

// consumeWhitespaceLong is the out-of-line half of [ConsumeWhitespace]. It
// skips whole vectors of whitespace while it can and leaves the rest to the
// portable scanner. Indentation before a token is typically one to a few
// dozen bytes, so this is usually a single vector.
func consumeWhitespaceLong(b []byte) int {
	if len(b) > 1 && b[0] == ' ' && b[1] > ' ' {
		return 1 // the space after a colon; see consumeWhitespaceScalar
	}
	var n int
	if useSIMD && len(b) >= vectorBytes {
		tables := loadClassTables(&whitespaceTables)
		for ; n+vectorBytes <= len(b); n += vectorBytes {
			// The class is whitespace, so the complement marks the first
			// byte that is not.
			if m := ^tables.members(archsimd.LoadUint8x32(b[n:])); m != 0 {
				archsimd.ClearAVXUpperBits()
				return n + bits.TrailingZeros32(m)
			}
		}
		archsimd.ClearAVXUpperBits()
	}
	return n + consumeWhitespaceScalar(b[n:])
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
