// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

import (
	"bytes"
	"testing"
)

func consumeSimpleStringScalar(b []byte) int {
	if len(b) == 0 || b[0] != '"' {
		return 0
	}
	n := 1
	for n < len(b) && b[n] < 128 && escapeASCII[b[n]] == 0 {
		n++
	}
	if n < len(b) && b[n] == '"' {
		return n + 1
	}
	return 0
}

func TestConsumeSimpleStringBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, 7, 15, 16, 31, 32, 33, 63, 64, 65, 127, 128, 129} {
		for pos := 0; pos <= size; pos++ {
			// Vary alignment and place every possible byte on both sides of each
			// vector boundary. Trailing data must not affect the closing quote.
			backing := bytes.Repeat([]byte{'a'}, size+96)
			b := backing[pos%32:][:size+34]
			b[0] = '"'
			b[size+1] = '"'
			for c := 0; c < 256; c++ {
				b[pos+1] = byte(c)
				got, want := ConsumeSimpleString(b), consumeSimpleStringScalar(b)
				if got != want {
					t.Fatalf("size=%d pos=%d byte=%02x: got %d, want %d", size, pos, c, got, want)
				}
			}
		}
	}
}

func FuzzConsumeSimpleString(f *testing.F) {
	for _, b := range [][]byte{nil, []byte(`"hello"`), []byte(`"你好"`), []byte(`"a\"b"`), bytes.Repeat([]byte{'a'}, 129)} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if got, want := ConsumeSimpleString(b), consumeSimpleStringScalar(b); got != want {
			t.Fatalf("got %d, want %d", got, want)
		}
	})
}
