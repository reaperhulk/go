// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

import "unicode/utf8"

// This file declares the byte classification scanners that sit at the heart of
// both the JSON decoder and encoder. Every scanner answers the same question:
// given a run of bytes, where does the first "interesting" byte occur?
// Everything before that index can be copied verbatim, which is what makes
// these loops worth vectorizing.
//
// Two character classes matter:
//
//	stringByte: bytes that terminate a raw JSON string body, namely
//	            a control character, '"', '\\', or any non-ASCII byte.
//	escapeByte: the stringByte class plus '&', '<', and '>', matching the
//	            conservative escapeASCII table used by the encoder.
//
// Each class has a scalar reference implementation here and (on supported
// platforms) a vectorized implementation that consumes a whole vector of input
// per iteration. The scalar versions are the source of truth: scan_test.go
// checks the two against each other for every input it can think of.
//
// The vectorized implementations use the nibble-classification trick from
// simdjson (https://github.com/simdjson/simdjson): rather than comparing each
// input byte against every member of the character class, two table lookups
// (one indexed by the low nibble, one by the high nibble) are ANDed together.
// A byte is a member of the class if and only if the AND is non-zero. Each bit
// of the table entries stands for one "high nibble row" of the 16x16 ASCII
// grid, so a class is representable this way as long as it needs no more than
// eight distinct rows, which is comfortably true for both classes above.
//
// The tables are laid out as [32]uint8 rather than [16]uint8 because the x86
// byte-shuffle instruction operates independently within each 128-bit half of
// a 256-bit vector, so each 16-entry table must be repeated twice.

// scanTables holds the two nibble lookup tables describing a character class,
// each repeated once per 128-bit lane.
type scanTables struct {
	lo [32]uint8 // indexed by the low nibble of the input byte
	hi [32]uint8 // indexed by the high nibble of the input byte
}

// dup16 repeats a 16-entry nibble table into both 128-bit lanes.
func dup16(t [16]uint8) (r [32]uint8) {
	copy(r[:16], t[:])
	copy(r[16:], t[:])
	return r
}

// Bit assignments for stringTables:
//
//	0x01: high nibble 0 (0x00-0x0F control characters)
//	0x02: high nibble 1 (0x10-0x1F control characters)
//	0x04: high nibble 2, low nibble 2 ('"')
//	0x08: high nibble 5, low nibble C ('\\')
//	0x80: high nibble 8-F (non-ASCII)
var stringTables = scanTables{
	lo: dup16([16]uint8{
		0x83, 0x83, 0x87, 0x83, 0x83, 0x83, 0x83, 0x83,
		0x83, 0x83, 0x83, 0x83, 0x8b, 0x83, 0x83, 0x83,
	}),
	hi: dup16([16]uint8{
		0x01, 0x02, 0x04, 0x00, 0x00, 0x08, 0x00, 0x00,
		0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80,
	}),
}

// Bit assignments for escapeTables:
//
//	0x01: high nibble 0 (0x00-0x0F control characters)
//	0x02: high nibble 1 (0x10-0x1F control characters)
//	0x04: high nibble 2, low nibble 2 or 6 ('"' and '&')
//	0x08: high nibble 3, low nibble C or E ('<' and '>')
//	0x10: high nibble 5, low nibble C ('\\')
//	0x80: high nibble 8-F (non-ASCII)
var escapeTables = scanTables{
	lo: dup16([16]uint8{
		0x83, 0x83, 0x87, 0x83, 0x83, 0x83, 0x87, 0x83,
		0x83, 0x83, 0x83, 0x83, 0x9b, 0x83, 0x8b, 0x83,
	}),
	hi: dup16([16]uint8{
		0x01, 0x02, 0x04, 0x08, 0x00, 0x10, 0x00, 0x00,
		0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80,
	}),
}

// shortScan is the number of bytes that the window scanners below examine
// before handing off to the out-of-line bulk scanner. Runs of copyable bytes
// in real JSON are usually very short: object names are a handful of bytes,
// escaped text breaks out every few bytes, and non-ASCII text breaks out on
// nearly every byte. Paying for a call, let alone for setting up a vector
// loop, on runs like those costs far more than it saves, so only runs that
// outlast the window are handed off.
const shortScan = 16

// scanStringByteWindow scans forward from index n over at most [shortScan]
// bytes of b and returns the index of the first byte that terminates a run of
// raw JSON string content. It returns n+shortScan if the run outlasts the
// window, and len(b) if it reaches the end of b first; a caller that needs to
// tell those apart compares the result against n+shortScan, which is cheaper
// than re-classifying the byte it stopped on.
//
// NOTE: The logic is kept simple, and takes an index rather than a subslice,
// so that this inlines into its callers. That is what keeps short strings off
// the call path entirely.
func scanStringByteWindow(b []byte, n int) int {
	// Truncating b rather than tracking a separate limit keeps the loop
	// condition in the form that lets the compiler drop the bounds check.
	if len(b) > n+shortScan {
		b = b[:n+shortScan]
	}
	for uint(len(b)) > uint(n) && stringByteTable[b[n]] == 0 {
		n++
	}
	return n
}

// scanEscapeByteWindow is [scanStringByteWindow] for the escapeByte class.
//
// NOTE: The logic is kept simple so that this inlines into its callers.
func scanEscapeByteWindow(b []byte, n int) int {
	if len(b) > n+shortScan {
		b = b[:n+shortScan]
	}
	for uint(len(b)) > uint(n) && escapeByteTable[b[n]] == 0 {
		n++
	}
	return n
}

// indexStringByte returns the index of the first byte of b that terminates a
// run of raw JSON string content, or len(b) if there is no such byte.
//
// This is the definition of what the hot loops in this package compute; those
// loops spell the window scan and the handoff out by hand rather than calling
// here, because a call is too expensive on the short runs they usually see.
func indexStringByte(b []byte) int {
	n := scanStringByteWindow(b, 0)
	if n == shortScan {
		n += indexStringByteLong(b[n:])
	}
	return n
}

// indexEscapeByte returns the index of the first byte of b that may need
// escaping when encoding a JSON string, or len(b) if there is no such byte.
//
// This is the definition of what the hot loops in this package compute; those
// loops spell the window scan and the handoff out by hand rather than calling
// here, because a call is too expensive on the short runs they usually see.
func indexEscapeByte(b []byte) int {
	n := scanEscapeByteWindow(b, 0)
	if n == shortScan {
		n += indexEscapeByteLong(b[n:])
	}
	return n
}

// stringByteTable and escapeByteTable are [isStringByte] and [isEscapeByte]
// as 256-entry lookup tables. A single indexed load is both smaller and
// faster than the run of comparisons that the predicates compile to, and the
// scanners are small enough that the difference decides whether they inline.
// TestScanTables checks that they agree with the predicates.
var (
	stringByteTable = classTable(isStringByte)
	escapeByteTable = classTable(isEscapeByte)
)

func classTable(in func(byte) bool) (t [256]uint8) {
	for i := range t {
		if in(byte(i)) {
			t[i] = 1
		}
	}
	return t
}

// isStringByte reports whether c terminates a run of raw JSON string content.
func isStringByte(c byte) bool {
	return c < ' ' || c == '"' || c == '\\' || c >= utf8.RuneSelf
}

// isEscapeByte reports whether c may need escaping when encoding a JSON string.
// It conservatively assumes EscapeForHTML and EscapeForJS.
func isEscapeByte(c byte) bool {
	return c >= utf8.RuneSelf || escapeASCII[c] > 0
}

// indexStringByteScalar is the reference implementation of [indexStringByte].
func indexStringByteScalar(b []byte) int {
	for i := 0; uint(len(b)) > uint(i); i++ {
		if stringByteTable[b[i]] != 0 {
			return i
		}
	}
	return len(b)
}

// indexEscapeByteScalar is the reference implementation of [indexEscapeByte].
func indexEscapeByteScalar(b []byte) int {
	for i := 0; uint(len(b)) > uint(i); i++ {
		if escapeByteTable[b[i]] != 0 {
			return i
		}
	}
	return len(b)
}

// classifyScalar reports whether c is a member of the class described by t.
// It exists so that scan_test.go can verify that the nibble tables agree with
// the predicates they are meant to encode.
func (t *scanTables) classifyScalar(c byte) bool {
	return t.lo[c&0x0f]&t.hi[c>>4] != 0
}
