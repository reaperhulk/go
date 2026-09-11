// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && !(goexperiment.simd && (amd64 || arm64))

package jsonwire

// simdEnabled reports whether this build contains a vectorized bulk scanner.
// It is a constant so that builds without one pay nothing for the check.
const simdEnabled = false

// simdHardware and useSIMD mirror the variables of the same name in the
// vectorized builds so that tests and benchmarks compile everywhere.
// They are always false here.
var (
	simdHardware = false
	useSIMD      = false
)

// indexStringByteLong is the out-of-line half of [indexStringByte].
func indexStringByteLong(b []byte) int { return indexStringByteScalar(b) }

// indexEscapeByteLong is the out-of-line half of [indexEscapeByte].
func indexEscapeByteLong(b []byte) int { return indexEscapeByteScalar(b) }

// ConsumeWhitespace consumes leading JSON whitespace per RFC 7159, section 2.
//
// This build has no vectorized whitespace scanner. On amd64 the plain loop
// measures as well as the bulk scalar scanner on runs of indentation and
// costs nothing on the single space after a colon, where going out of line
// showed up as +6-15% on the indented testdata, so it stays the loop it
// always was. The builds with a vector path define their own
// ConsumeWhitespace in scan_whitespace_simd.go.
func ConsumeWhitespace(b []byte) (n int) {
	// NOTE: The arguments and logic are kept simple to keep this inlinable.
	for len(b) > n && (b[n] == ' ' || b[n] == '\t' || b[n] == '\r' || b[n] == '\n') {
		n++
	}
	return n
}
