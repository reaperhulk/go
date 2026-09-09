// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

import (
	"bytes"
	"encoding/json/internal/jsonflags"
	"testing"
)

func TestAppendQuoteASCIIPrefix(t *testing.T) {
	suffixes := []string{"你好", "\u2028\u2029", "\xff\xfe", "\"\\\n"}
	for c := 0; c < 256; c++ {
		suffixes = append(suffixes, string([]byte{byte(c)}))
	}
	for _, opts := range []jsonflags.Bools{0, jsonflags.EscapeForHTML, jsonflags.EscapeForJS, jsonflags.AllowInvalidUTF8, jsonflags.AnyEscape | jsonflags.AllowInvalidUTF8} {
		var flags jsonflags.Flags
		flags.Set(opts | 1)
		for _, size := range []int{0, 1, 31, 32, 33, 63, 64, 65, 127, 128, 129} {
			prefix := bytes.Repeat([]byte{'a'}, size)
			for _, suffix := range suffixes {
				// The short suffix uses the scalar path; prefixing ordinary ASCII must
				// preserve its escaping and error behavior at every vector boundary.
				quoted, wantErr := AppendQuote(nil, []byte(suffix), &flags)
				want := append(append([]byte{'"'}, prefix...), quoted[1:]...)
				src := append(bytes.Clone(prefix), suffix...)
				got, gotErr := AppendQuote(nil, src, &flags)
				if !bytes.Equal(got, want) || gotErr != wantErr {
					t.Fatalf("size=%d suffix=%q flags=%v: got (%q,%v), want (%q,%v)", size, suffix, opts, got, gotErr, want, wantErr)
				}
			}
		}
	}
}
