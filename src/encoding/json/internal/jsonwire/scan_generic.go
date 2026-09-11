// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

// simdEnabled reports whether this build contains a vectorized bulk scanner.
// It is a constant so that builds without one pay nothing for the check.
const simdEnabled = false

// simdHardware reports whether the CPU can run the vectorized bulk scanner,
// and useSIMD whether it is currently in use; benchmarks flip the latter to
// compare implementations in one process. Neither is ever true here.
var (
	simdHardware = false
	useSIMD      = false
)

// indexStringByteLong is the out-of-line half of [indexStringByte].
func indexStringByteLong(b []byte) int { return indexStringByteScalar(b) }

// indexEscapeByteLong is the out-of-line half of [indexEscapeByte].
func indexEscapeByteLong(b []byte) int { return indexEscapeByteScalar(b) }
