// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestScanPredicates pins the two character classes to the definitions that
// the rest of the package relies on.
func TestScanPredicates(t *testing.T) {
	for i := range 256 {
		c := byte(i)
		want := c < ' ' || c == '"' || c == '\\' || c >= utf8.RuneSelf
		if got := isStringByte(c); got != want {
			t.Errorf("isStringByte(%#02x) = %v, want %v", c, got, want)
		}
		wantEscape := want || c == '&' || c == '<' || c == '>'
		if got := isEscapeByte(c); got != wantEscape {
			t.Errorf("isEscapeByte(%#02x) = %v, want %v", c, got, wantEscape)
		}
	}
}

// scanFuncs pairs each production scanner with its scalar reference.
var scanFuncs = []struct {
	name  string
	got   func([]byte) int
	want  func([]byte) int
	class func(byte) bool
}{
	{"StringByte", indexStringByte, indexStringByteScalar, isStringByte},
	{"EscapeByte", indexEscapeByte, indexEscapeByteScalar, isEscapeByte},
}

// TestIndexScanners exhaustively checks the scanners against their scalar
// references for inputs that place a special byte at every offset of every
// length up to a few vectors' worth, and for inputs with no special byte.
func TestIndexScanners(t *testing.T) {
	if simdEnabled && !useSIMD {
		t.Skip("vectorized scanners unavailable on this CPU")
	}
	specials := []byte{0x00, 0x0a, 0x1f, '"', '&', '<', '>', '\\', 0x7f, 0x80, 0xc3, 0xff}
	for _, f := range scanFuncs {
		t.Run(f.name, func(t *testing.T) {
			for n := range 130 {
				plain := bytes.Repeat([]byte("a"), n)
				if got, want := f.got(plain), f.want(plain); got != want {
					t.Fatalf("len=%d plain: got %d, want %d", n, got, want)
				}
				for _, sp := range specials {
					for at := range n {
						b := bytes.Repeat([]byte("a"), n)
						b[at] = sp
						got, want := f.got(b), f.want(b)
						if got != want {
							t.Fatalf("len=%d at=%d special=%#02x: got %d, want %d", n, at, sp, got, want)
						}
						if want < len(b) && !f.class(b[want]) {
							t.Fatalf("len=%d at=%d: reference returned non-member index", n, at)
						}
					}
				}
			}
		})
	}
}

// TestIndexScannersRandom compares the scanners against their references on
// random inputs drawn from a byte distribution resembling real JSON.
func TestIndexScannersRandom(t *testing.T) {
	if simdEnabled && !useSIMD {
		t.Skip("vectorized scanners unavailable on this CPU")
	}
	rn := rand.New(rand.NewSource(1))
	for _, f := range scanFuncs {
		t.Run(f.name, func(t *testing.T) {
			for range 20000 {
				b := make([]byte, rn.Intn(300))
				for i := range b {
					switch rn.Intn(16) {
					case 0:
						b[i] = byte(rn.Intn(256)) // any byte, including specials
					default:
						b[i] = byte(' ' + rn.Intn('~'-' '+1))
					}
				}
				if got, want := f.got(b), f.want(b); got != want {
					t.Fatalf("%q: got %d, want %d", b, got, want)
				}
			}
		})
	}
}

// TestIndexScannersUnaligned checks that the scanners behave the same at every
// alignment, since the vectorized loop loads unaligned.
func TestIndexScannersUnaligned(t *testing.T) {
	if simdEnabled && !useSIMD {
		t.Skip("vectorized scanners unavailable on this CPU")
	}
	base := []byte(strings.Repeat("abcdefgh", 40) + `"` + strings.Repeat("x", 40))
	for _, f := range scanFuncs {
		t.Run(f.name, func(t *testing.T) {
			for off := range len(base) {
				b := base[off:]
				if got, want := f.got(b), f.want(b); got != want {
					t.Fatalf("off=%d: got %d, want %d", off, got, want)
				}
			}
		})
	}
}

// FuzzIndexScanners checks the vectorized scanners against their scalar
// references on arbitrary input.
func FuzzIndexScanners(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("hello world"))
	f.Add([]byte(strings.Repeat("hello world", 32)))
	f.Add([]byte(strings.Repeat("a", 31) + `"` + strings.Repeat("b", 64)))
	f.Add([]byte(strings.Repeat("a", 64) + "\x00"))
	f.Add([]byte("  日本語"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if got, want := indexStringByte(b), indexStringByteScalar(b); got != want {
			t.Fatalf("indexStringByte(%q) = %d, want %d", b, got, want)
		}
		if got, want := indexEscapeByte(b), indexEscapeByteScalar(b); got != want {
			t.Fatalf("indexEscapeByte(%q) = %d, want %d", b, got, want)
		}
	})
}
