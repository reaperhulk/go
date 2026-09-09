// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

import (
	"io"
	"strings"
	"testing"
)

func TestConsumeStringASCIIRuns(t *testing.T) {
	for _, suffix := range []string{"世", `\n`, `\u0061`, `\ud83d\ude00`, "&<>", `\u0000`} {
		var wantFlags ValueFlags
		if _, err := ConsumeString(&wantFlags, []byte(`"`+suffix+`"`), true); err != nil {
			t.Fatal(err)
		}
		for _, size := range []int{0, 1, 31, 32, 33, 63, 64, 65, 127, 128, 129} {
			b := []byte(`"` + strings.Repeat(strings.Repeat("a", size)+suffix, 3) + `"`)
			// Resume at every possible truncation, including within a vector,
			// UTF-8 rune, escape sequence, and UTF-16 surrogate pair.
			for split := 0; split <= len(b); split++ {
				var flags ValueFlags
				n, err := ConsumeStringResumable(&flags, b[:split], 0, true)
				if err == io.ErrUnexpectedEOF {
					n, err = ConsumeStringResumable(&flags, b, n, true)
				}
				if n != len(b) || err != nil || flags != wantFlags {
					t.Fatalf("suffix=%q size=%d split=%d: got (%d,%v,%v), want (%d,nil,%v)", suffix, size, split, n, err, flags, len(b), wantFlags)
				}
			}
		}
	}
}
