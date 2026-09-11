// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && goexperiment.simd && amd64

package jsonwire

import (
	"math/bits"
	"simd/archsimd"
)

// utf8SIMD reports whether this build can validate UTF-8 a vector at a time.
const utf8SIMD = true

// This file implements the UTF-8 validation algorithm from simdjson
// (Keiser and Lemire, "Validating UTF-8 In Less Than One Instruction Per
// Byte", https://arxiv.org/abs/2010.03090), and uses it to let the string
// scanners skip non-ASCII text at vector speed instead of decoding it a rune
// at a time.
//
// The algorithm classifies each byte together with the byte before it using
// three nibble lookups: the high and low nibble of the previous byte, and the
// high nibble of the current byte. Each lookup yields a set of error flags
// that the (previous, current) pair could exhibit; the flags common to all
// three are the errors it does exhibit. That catches every malformed pair:
// a lead byte followed by something other than a continuation, an ASCII byte
// followed by a continuation, overlong two-byte forms, overlong three-byte
// forms, surrogates, code points above U+10FFFF, and overlong four-byte
// forms. The one thing pairs cannot see is whether a continuation byte is
// the third or fourth of its sequence, so a separate check looks two and
// three bytes back for the leads that demand one.
//
// Errors are reported at the position of the *current* byte of the offending
// pair, which the callers rely on: an error flagged at position i says
// something is wrong with b[i] given the bytes before it, never with bytes
// after it.

// Error flags. The comments give the (previous, current) bit patterns.
const (
	utf8TooShort     = 1 << 0 // 11______ 0_______, 11______ 11______
	utf8TooLong      = 1 << 1 // 0_______ 10______
	utf8Overlong3    = 1 << 2 // 11100000 100_____
	utf8TooLarge     = 1 << 3 // 11110100 1001____, 11110100 101_____, 11110101 1001____, ...
	utf8Surrogate    = 1 << 4 // 11101101 101_____
	utf8Overlong2    = 1 << 5 // 1100000_ 10______
	utf8TooLarge1000 = 1 << 6 // 11110101 1000____, 1111011_ 1000____, 11111___ 1000____
	utf8Overlong4    = 1 << 6 // 11110000 1000____
	utf8TwoConts     = 1 << 7 // 10______ 10______
	utf8Carry        = utf8TooShort | utf8TooLong | utf8TwoConts
)

var (
	// utf8Byte1High is indexed by the high nibble of the previous byte.
	utf8Byte1High = dup16([16]uint8{
		// 0_______: ASCII, may not be followed by a continuation.
		utf8TooLong, utf8TooLong, utf8TooLong, utf8TooLong,
		utf8TooLong, utf8TooLong, utf8TooLong, utf8TooLong,
		// 10______: continuation.
		utf8TwoConts, utf8TwoConts, utf8TwoConts, utf8TwoConts,
		// 1100____ and 1101____: two-byte leads.
		utf8TooShort | utf8Overlong2,
		utf8TooShort,
		// 1110____: three-byte lead.
		utf8TooShort | utf8Overlong3 | utf8Surrogate,
		// 1111____: four-byte lead, or invalid.
		utf8TooShort | utf8TooLarge | utf8TooLarge1000 | utf8Overlong4,
	})
	// utf8Byte1Low is indexed by the low nibble of the previous byte.
	utf8Byte1Low = dup16([16]uint8{
		// ____0000
		utf8Carry | utf8Overlong3 | utf8Overlong2 | utf8Overlong4,
		// ____0001
		utf8Carry | utf8Overlong2,
		// ____001_
		utf8Carry, utf8Carry,
		// ____0100
		utf8Carry | utf8TooLarge,
		// ____0101
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		// ____011_
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		// ____1___
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		// ____1101: also the surrogate lead 0xED.
		utf8Carry | utf8TooLarge | utf8TooLarge1000 | utf8Surrogate,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
	})
	// utf8Byte2High is indexed by the high nibble of the current byte.
	utf8Byte2High = dup16([16]uint8{
		// 0_______: ASCII, may not follow a lead.
		utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort,
		utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort,
		// 1000____
		utf8TooLong | utf8Overlong2 | utf8TwoConts | utf8Overlong3 | utf8TooLarge1000 | utf8Overlong4,
		// 1001____
		utf8TooLong | utf8Overlong2 | utf8TwoConts | utf8Overlong3 | utf8TooLarge,
		// 101_____
		utf8TooLong | utf8Overlong2 | utf8TwoConts | utf8Surrogate | utf8TooLarge,
		utf8TooLong | utf8Overlong2 | utf8TwoConts | utf8Surrogate | utf8TooLarge,
		// 11______: a lead, may not follow a lead.
		utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort,
	})

	// utf8IncompleteMax is the largest value each of the last three bytes of
	// a vector may hold without starting a sequence that runs past the end
	// of it: a four-byte lead in the third-to-last position, a three-byte
	// lead in the second-to-last, or any lead in the last.
	utf8IncompleteMax = [32]uint8{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xf0 - 1, 0xe0 - 1, 0xc0 - 1,
	}
)

// skipUTF8Long is the UTF-8 aware bulk scanner. Starting at index n of b, it
// accepts ASCII bytes that are not members of the class described by t, and
// non-ASCII bytes that form valid UTF-8, a vector at a time. It returns the
// index at which the caller's scalar loop should resume, which is either a
// member of the class, the start of a sequence it could not accept because
// the sequence was malformed or ran past what it examined, or n itself if
// there were not enough bytes left to examine a whole vector.
//
// Every byte before the returned index has been accepted, and the index is
// on a sequence boundary, so a scalar decoder resuming there sees exactly
// what it would have seen scanning from n. Whenever the vector scanner stops
// for any reason, it backs up over the sequence in progress rather than
// splitting hairs about which byte of it was wrong: the scalar code is the
// reference and re-derives the exact answer from the boundary.
//
// All of the vector state lives in local variables rather than a struct so
// that the compiler keeps it in registers; a struct with this many fields is
// not SSA-able and every use of it would go through memory.
func skipUTF8Long(t *scanTables, b []byte, n int) int {
	if len(b)-n < vectorBytes {
		return n
	}
	tables := loadClassTables(t)
	byte1High := archsimd.LoadUint8x32Array(&utf8Byte1High)
	byte1Low := archsimd.LoadUint8x32Array(&utf8Byte1Low)
	byte2High := archsimd.LoadUint8x32Array(&utf8Byte2High)
	incompleteMax := archsimd.LoadUint8x32Array(&utf8IncompleteMax)
	nibble := archsimd.BroadcastUint8x32(0x0f)
	hiBit := archsimd.BroadcastUint8x32(0x80)
	sub60 := archsimd.BroadcastUint8x32(0xe0 - 0x80)
	sub70 := archsimd.BroadcastUint8x32(0xf0 - 0x80)
	var zero archsimd.Uint8x32
	var zeroInt archsimd.Int8x32

	var prev archsimd.Uint8x32 // the previous vector, zero (ASCII) before the first
	prevIncomplete := false    // prev ended inside a sequence

	start := n
	for ; n+vectorBytes <= len(b); n += vectorBytes {
		v := archsimd.LoadUint8x32(b[n:])
		hits := tables.members(v)
		if nonASCII := v.AsInt8x32().Less(zeroInt).ToBits(); nonASCII == 0 {
			// All ASCII: the only possible error is a sequence left open by
			// the previous vector, which this one fails to continue.
			if prevIncomplete {
				hits |= 1
			}
			prev, prevIncomplete = v, false
		} else {
			// prevN is v shifted right by N bytes with the bottom N bytes
			// taken from the end of prev. VPALIGNR works within each 128-bit
			// lane, so first build (prev.hi, v.lo), the vector of what each
			// lane of v was preceded by.
			carried := prev.ConcatPermute128Scalars(1, 2, v)
			prev1 := v.ConcatShiftBytesRightGrouped(carried, 16-1)
			prev2 := v.ConcatShiftBytesRightGrouped(carried, 16-2)
			prev3 := v.ConcatShiftBytesRightGrouped(carried, 16-3)

			// Errors that a (previous, current) pair exhibits are the flags
			// common to the three lookups.
			p1hi := byte1High.PermuteOrZeroGrouped(
				prev1.ReshapeToUint16s().ShiftAllRight(4).ReshapeToUint8s().And(nibble).AsInt8x32())
			p1lo := byte1Low.PermuteOrZeroGrouped(prev1.And(nibble).AsInt8x32())
			curHi := byte2High.PermuteOrZeroGrouped(
				v.ReshapeToUint16s().ShiftAllRight(4).ReshapeToUint8s().And(nibble).AsInt8x32())
			special := p1hi.And(p1lo).And(curHi)

			// A byte two or three back that is a three- or four-byte lead
			// demands a continuation here. Those leads are >= 0xe0 and
			// >= 0xf0, so saturating subtraction leaves bit 7 set exactly for
			// them. utf8TwoConts, also bit 7, is set in special exactly when
			// this byte is a continuation following a continuation, and the
			// two must agree.
			mustBeCont := prev2.SubSaturated(sub60).Or(prev3.SubSaturated(sub70)).And(hiBit)
			errs := mustBeCont.Xor(special)
			hits |= ^errs.Equal(zero).ToBits()

			prev = v
			prevIncomplete = !v.SubSaturated(incompleteMax).IsZero()
		}
		if hits != 0 {
			archsimd.ClearAVXUpperBits()
			return utf8Boundary(b, start, n+bits.TrailingZeros32(hits))
		}
	}
	archsimd.ClearAVXUpperBits()
	return utf8Boundary(b, start, n)
}

// utf8Boundary backs p up to the start of the UTF-8 sequence in progress at
// p, but not before start. Continuation bytes are 10xxxxxx and a sequence has
// at most three of them, so this steps over up to three continuations and
// then over one lead byte if there is one.
func utf8Boundary(b []byte, start, p int) int {
	for i := 0; i < 3 && p > start && b[p-1]&0xc0 == 0x80; i++ {
		p--
	}
	if p > start && b[p-1] >= 0xc0 {
		p--
	}
	return p
}
