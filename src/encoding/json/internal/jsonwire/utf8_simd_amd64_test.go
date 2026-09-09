// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && goexperiment.simd && amd64

package jsonwire

import (
	"bytes"
	"encoding/json/internal/jsonflags"
	"io"
	"simd/archsimd"
	"testing"
	"unicode/utf8"
)

func scalarUTF8Prefix(b []byte) (n int) {
	for n < len(b) {
		if c := b[n]; c < utf8.RuneSelf {
			if escapeASCII[c] != 0 {
				break
			}
			n++
			continue
		}
		r, size := utf8.DecodeRune(b[n:])
		if size == 1 || r == '\u2028' || r == '\u2029' {
			break
		}
		n += size
	}
	return n
}

func checkUTF8Prefix(t *testing.T, b []byte) {
	t.Helper()
	n := consumeUTF8Prefix(b)
	if n < 0 || n > scalarUTF8Prefix(b) || !utf8.Valid(b[:n]) {
		t.Fatalf("prefix(%x) = %d, scalar = %d", b, n, scalarUTF8Prefix(b))
	}
}

func TestUTF8PrefixEveryRune(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 unavailable")
	}
	for r := rune(0x80); r <= utf8.MaxRune; r++ {
		if !utf8.ValidRune(r) || r == '\u2028' || r == '\u2029' {
			continue
		}
		b := bytes.Repeat([]byte(string(r)), 40)
		n := consumeUTF8Prefix(b)
		if n < len(b)-31 || n > len(b) || !utf8.Valid(b[:n]) {
			t.Fatalf("rune U+%04X: consumed %d of %d bytes", r, n, len(b))
		}
	}
}

func TestUTF8PrefixByteBoundaries(t *testing.T) {
	for _, s := range []string{"é", "世", "😀", "é a世😀"} {
		b := bytes.Repeat([]byte(s), 40)
		for end := 0; end <= len(b); end++ {
			checkUTF8Prefix(t, b[:end])
		}
		for pos := 0; pos < 68; pos++ {
			old := b[pos]
			for c := 0; c < 256; c++ {
				b[pos] = byte(c)
				checkUTF8Prefix(t, b)
			}
			b[pos] = old
		}
	}
}

func TestUTF8PrefixMalformedSequences(t *testing.T) {
	// Vary both leading bytes exhaustively, then exercise the continuation
	// classes that distinguish lengths, overlong forms, and Unicode limits.
	b := append(bytes.Repeat([]byte("é"), 16), bytes.Repeat([]byte("a"), 68)...)
	edges := []byte{0x7f, 0x80, 0x8f, 0x90, 0x9f, 0xa0, 0xbf, 0xc0}
	for first := 0x80; first < 256; first++ {
		b[32] = byte(first)
		for second := 0; second < 256; second++ {
			b[33] = byte(second)
			for _, third := range edges {
				b[34] = third
				for _, fourth := range edges {
					b[35] = fourth
					checkUTF8Prefix(t, b)
				}
			}
		}
	}
}

func FuzzUTF8Prefix(f *testing.F) {
	for _, s := range []string{"", "é世😀", "\xc0\x80", "\xe0\x80\x80", "\xed\xa0\x80", "\xf0\x80\x80\x80", "\xf4\x90\x80\x80", "\u2028\u2029"} {
		f.Add(bytes.Repeat([]byte(s), 40))
	}
	f.Fuzz(func(t *testing.T, b []byte) { checkUTF8Prefix(t, b) })
}

func FuzzQuoteUTF8(f *testing.F) {
	f.Add(string(bytes.Repeat([]byte("é世😀"), 20)), byte(0))
	f.Add("\xe0\x80\x80<&>\u2028\u2029\xff\n", byte(3))
	f.Fuzz(func(t *testing.T, s string, option byte) {
		opts := []jsonflags.Bools{0, jsonflags.EscapeForHTML, jsonflags.EscapeForJS, jsonflags.AnyEscape | jsonflags.AllowInvalidUTF8}
		var flags jsonflags.Flags
		flags.Set(opts[int(option)%len(opts)] | 1)
		b := []byte(s)
		want := []byte{'"'}
		var wantErr error
		for i := 0; i < len(b); {
			_, size := utf8.DecodeRune(b[i:])
			// Individual runes always take the scalar quoting path.
			part, err := AppendQuote(nil, b[i:i+size], &flags)
			want = append(want, part[1:len(part)-1]...)
			if err != nil {
				wantErr = err
			}
			i += size
		}
		want = append(want, '"')
		got, err := AppendQuote(nil, b, &flags)
		if !bytes.Equal(got, want) || err != wantErr {
			t.Fatalf("quote %x: got (%q,%v), want (%q,%v)", b, got, err, want, wantErr)
		}
		// One-byte increments exercise the scalar decoder and its flags;
		// the full input exercises block validation and must agree.
		var scalarFlags, blockFlags ValueFlags
		n := 0
		for end := 1; end <= len(want); end++ {
			n, err = ConsumeStringResumable(&scalarFlags, want[:end], n, true)
			if err != nil && err != io.ErrUnexpectedEOF {
				t.Fatal(err)
			}
		}
		m, blockErr := ConsumeString(&blockFlags, want, true)
		if m != n || blockErr != err || blockFlags != scalarFlags {
			t.Fatalf("consume %q: block (%d,%v,%v), scalar (%d,%v,%v)", want, m, blockErr, blockFlags, n, err, scalarFlags)
		}
	})
}
