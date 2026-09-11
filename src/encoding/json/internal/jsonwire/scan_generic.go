// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && !(goexperiment.simd && amd64)

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

// consumeWhitespaceLong is the out-of-line half of [ConsumeWhitespace]. This
// wrapper inlines into it, so the call it makes goes straight to the scanner.
func consumeWhitespaceLong(b []byte) int { return consumeWhitespaceScalar(b) }

// utf8SIMD reports whether this build can validate UTF-8 a vector at a time.
const utf8SIMD = false

func skipUTF8Long(t *scanTables, b []byte, n int) int { return n }
