// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

import (
	"bytes"
	"math/rand"
	"testing"
	"unicode/utf8"
)

// utf8ScalarStop is what the scalar loops do with a run: it returns the index
// of the first byte that they cannot simply step over, which is a member of
// the class or the start of an invalid or truncated UTF-8 sequence.
func utf8ScalarStop(t *scanTables, b []byte, n int) int {
	for n < len(b) {
		c := b[n]
		if c < utf8.RuneSelf {
			if t.classifyScalar(c) {
				return n
			}
			n++
			continue
		}
		if t.classifyScalar(c) {
			return n
		}
		r, rn := utf8.DecodeRune(b[n:])
		if r == utf8.RuneError && rn == 1 {
			return n
		}
		n += rn
	}
	return n
}

// checkSkipUTF8 verifies the contract of skipUTF8Long on b starting at n:
// it may stop early, but only at a sequence boundary that the scalar loop
// would also reach, and never more than one sequence short of where the
// scalar loop stops unless it ran out of whole vectors.
func checkSkipUTF8(t *testing.T, tables *scanTables, b []byte, n int) {
	t.Helper()
	got := skipUTF8Long(tables, b, n)
	want := utf8ScalarStop(tables, b, n)
	if got < n || got > want {
		t.Fatalf("skipUTF8Long(%q, %d) = %d, scalar stops at %d", b, n, got, want)
	}
	// Everything accepted must be exactly what the scalar loop would have
	// stepped over, and the resume point must be on a sequence boundary.
	if s := utf8ScalarStop(tables, b[:got], n); s != got {
		t.Fatalf("skipUTF8Long(%q, %d) = %d, but scalar stops inside that at %d", b, n, got, s)
	}
	// Progress: the only reasons to stop short are the trailing partial
	// vector and backing up over the sequence in progress.
	if utf8SIMD && simdHardware && got < want && got < len(b)-vectorBytes-utf8.UTFMax && want-got > utf8.UTFMax {
		t.Fatalf("skipUTF8Long(%q, %d) = %d, want %d: stopped too early", b, n, got, want)
	}
}

var utf8TestTables = []*scanTables{&utf8StringTables, &utf8EscapeTables}

func TestUTF8Tables(t *testing.T) {
	for i := range 256 {
		c := byte(i)
		if got, want := utf8StringTables.classifyScalar(c), isUTF8StringByte(c); got != want {
			t.Errorf("utf8StringTables.classifyScalar(%#02x) = %v, want %v", c, got, want)
		}
		if got, want := utf8EscapeTables.classifyScalar(c), isUTF8EscapeByte(c); got != want {
			t.Errorf("utf8EscapeTables.classifyScalar(%#02x) = %v, want %v", c, got, want)
		}
	}
}

// TestSkipUTF8Sequences embeds every two-byte and three-byte sequence, and a
// broad sample of four-byte and longer ones, valid and invalid alike, in
// ASCII text at offsets that place them at the start of a vector, straddling
// a vector boundary, and at the end of one.
func TestSkipUTF8Sequences(t *testing.T) {
	if !utf8SIMD || !simdHardware {
		t.Skip("no vectorized UTF-8 validation in this build or on this CPU")
	}
	var seqs [][]byte
	for c0 := 0x80; c0 < 0x100; c0++ {
		for c1 := 0; c1 < 0x100; c1++ {
			seqs = append(seqs, []byte{byte(c0), byte(c1)})
		}
	}
	for c0 := 0xe0; c0 < 0x100; c0++ {
		for c1 := 0; c1 < 0x100; c1++ {
			for _, c2 := range []byte{0x00, 0x41, 0x7f, 0x80, 0x9f, 0xa0, 0xbf, 0xc0, 0xe0, 0xff} {
				seqs = append(seqs, []byte{byte(c0), byte(c1), c2})
				for _, c3 := range []byte{0x41, 0x80, 0xbf, 0xc2} {
					seqs = append(seqs, []byte{byte(c0), byte(c1), c2, c3})
				}
			}
		}
	}
	// Every valid rune, too.
	for r := rune(0x80); r <= utf8.MaxRune; r += 7 {
		if utf8.ValidRune(r) {
			seqs = append(seqs, utf8.AppendRune(nil, r))
		}
	}
	for _, tables := range utf8TestTables {
		for _, seq := range seqs {
			for _, pre := range []int{0, 1, 29, 30, 31, 32, 33} {
				b := append(bytes.Repeat([]byte("a"), pre), seq...)
				b = append(b, bytes.Repeat([]byte("b"), 40)...)
				b = append(b, '"')
				b = append(b, bytes.Repeat([]byte("c"), 40)...)
				checkSkipUTF8(t, tables, b, 0)
			}
		}
	}
}

// TestSkipUTF8Random checks dense non-ASCII text with a sprinkling of
// structural bytes and corruptions, starting from every offset.
func TestSkipUTF8Random(t *testing.T) {
	if !utf8SIMD || !simdHardware {
		t.Skip("no vectorized UTF-8 validation in this build or on this CPU")
	}
	rn := rand.New(rand.NewSource(7))
	for i := range 3000 {
		var b []byte
		for len(b) < 64+rn.Intn(200) {
			switch rn.Intn(24) {
			case 0:
				b = append(b, '"')
			case 1:
				b = append(b, '\\')
			case 2:
				b = append(b, byte(rn.Intn(0x20)))
			case 3:
				b = append(b, byte(0x80+rn.Intn(0x80))) // a stray non-ASCII byte
			case 4, 5, 6:
				b = append(b, byte(' '+rn.Intn(95)))
			default:
				r := rune(rn.Intn(utf8.MaxRune + 1))
				if !utf8.ValidRune(r) {
					r = 'é'
				}
				b = utf8.AppendRune(b, r)
			}
		}
		if i%3 == 0 { // corrupt one byte
			b[rn.Intn(len(b))] = byte(rn.Intn(256))
		}
		for _, tables := range utf8TestTables {
			for n := 0; n < len(b); n += 1 + rn.Intn(3) {
				checkSkipUTF8(t, tables, b, n)
			}
		}
	}
}

func FuzzSkipUTF8(f *testing.F) {
	f.Add([]byte("héllo wörld, this is some text that is long enough to fill a vector"), 0)
	f.Add([]byte("日本語のテキストは三バイトの文字で構成されていますし、それは長い。"), 0)
	f.Add([]byte("😀 emoji 😀 emoji 😀 emoji 😀 emoji 😀 emoji 😀 emoji 😀 emoji\""), 3)
	f.Fuzz(func(t *testing.T, b []byte, n int) {
		if n < 0 || n > len(b) {
			return
		}
		for _, tables := range utf8TestTables {
			checkSkipUTF8(t, tables, b, n)
		}
	})
}
