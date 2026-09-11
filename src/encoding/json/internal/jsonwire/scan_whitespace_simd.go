// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && goexperiment.simd && (amd64 || arm64)

package jsonwire

// ConsumeWhitespace consumes leading JSON whitespace per RFC 7159, section 2.
func ConsumeWhitespace(b []byte) int {
	// NOTE: The arguments and logic are kept simple to keep this inlinable.
	// Minified documents have no whitespace between tokens, and that case is
	// decided here without a call. Every JSON whitespace byte is <= ' ', so
	// anything at or below it (which, in valid JSON, is whitespace) goes out
	// of line, where runs of indentation are skipped a vector at a time.
	if len(b) > 0 && b[0] <= ' ' {
		return consumeWhitespaceLong(b)
	}
	return 0
}
