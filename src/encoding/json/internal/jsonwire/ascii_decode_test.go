// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

import (
	"encoding/json/internal/jsonflags"
	"io"
	"strconv"
	"strings"
	"testing"
)

func FuzzAppendUnquoteASCIIRuns(f *testing.F) {
	f.Add("hello")
	f.Add(strings.Repeat("a", 64) + "\n" + strings.Repeat("b", 64) + "世\xff")
	f.Fuzz(func(t *testing.T, s string) {
		var flags jsonflags.Flags
		flags.Set(jsonflags.AllowInvalidUTF8 | 1)
		quoted, err := AppendQuote(nil, []byte(s), &flags)
		if err != nil {
			t.Fatal(err)
		}
		// strconv supplies an independent reference for the valid JSON
		// string produced above, including replacement of invalid UTF-8.
		want, err := strconv.Unquote(string(quoted))
		if err != nil {
			t.Fatal(err)
		}
		got, err := AppendUnquote(nil, quoted)
		if string(got) != want || err != nil {
			t.Fatalf("unquote %q: got (%q,%v), want %q", quoted, got, err, want)
		}
	})
}

func TestConsumeStringASCIIRuns(t *testing.T) {
	for _, suffix := range []string{"世", `\n`, `\u0061`, `\ud83d\ude00`, "&<>", `\u0000`} {
		var wantFlags ValueFlags
		if _, err := ConsumeString(&wantFlags, []byte(`"`+suffix+`"`), true); err != nil {
			t.Fatal(err)
		}
		unquoted, err := AppendUnquote(nil, []byte(`"`+suffix+`"`))
		if err != nil {
			t.Fatal(err)
		}
		for _, size := range []int{0, 1, 31, 32, 33, 63, 64, 65, 127, 128, 129} {
			b := []byte(`"` + strings.Repeat(strings.Repeat("a", size)+suffix, 3) + `"`)
			want := strings.Repeat(strings.Repeat("a", size)+string(unquoted), 3)
			if got, err := AppendUnquote(nil, b); string(got) != want || err != nil {
				t.Fatalf("suffix=%q size=%d: unquote got (%q,%v), want %q", suffix, size, got, err, want)
			}
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
