// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package jsonwire

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"encoding/json/internal/jsonflags"
	"encoding/json/internal/jsonperf"
)

// scanLengths are the run lengths that the scanners are measured over.
//
// Real JSON is dominated by the short end: object names and enum-like values
// are usually well under one vector wide. A vectorized scanner only pays off
// overall if it holds its own down there, so the short lengths matter as much
// as the long ones and both are always reported.
var scanLengths = []int{0, 1, 2, 4, 8, 16, 24, 32, 48, 64, 128, 512, 4096}

// modes are the two implementations, which are compared within one process so
// that the comparison is not confounded by codegen differences between builds.
// Sub-benchmark names use the key=value form throughout so that benchstat can
// pivot on them: "benchstat -col /mode" puts the two implementations
// side by side.
var modes = [...]struct {
	name string
	simd bool
}{{"Scalar", false}, {"SIMD", true}}

var (
	sinkInt  int
	sinkBool bool
)

// benchScan runs fn over an input of every length in [scanLengths], under both
// the scalar and the vectorized scanner.
func benchScan(b *testing.B, input func(n int) []byte, fn func([]byte)) {
	for _, m := range modes {
		b.Run("mode="+m.name, func(b *testing.B) {
			for _, n := range scanLengths {
				b.Run(fmt.Sprintf("len=%d", n), func(b *testing.B) {
					if m.simd && !simdHardware {
						b.Skip("no vectorized scanner in this build or on this CPU")
					}
					old := useSIMD
					useSIMD = m.simd
					defer func() { useSIMD = old }()

					src := input(n)
					size := max(len(src), 1)
					b.SetBytes(int64(size))
					b.ReportAllocs()
					jsonperf.Measure(b, size, func() {
						for b.Loop() {
							fn(src)
						}
					})
				})
			}
		})
	}
}

// plainRun is n bytes that need no escaping, followed by the closing quote and
// enough trailing input to model a string sitting inside a larger buffer,
// which is how the decoder always sees one.
func plainRun(n int) []byte {
	b := append(bytes.Repeat([]byte("a"), n), '"')
	return append(b, bytes.Repeat([]byte("z"), 64)...)
}

func BenchmarkIndexStringByte(b *testing.B) {
	benchScan(b, plainRun, func(src []byte) { sinkInt = indexStringByte(src) })
}

func BenchmarkIndexEscapeByte(b *testing.B) {
	benchScan(b, plainRun, func(src []byte) { sinkInt = indexEscapeByte(src) })
}

// BenchmarkConsumeWhitespace models the indentation before a token: n
// whitespace bytes followed by the token and more of the document.
func BenchmarkConsumeWhitespace(b *testing.B) {
	indent := func(n int) []byte {
		ws := append([]byte("\n"), bytes.Repeat([]byte(" "), max(n-1, 0))...)[:n]
		return append(ws, []byte(`"name": "value", `+strings.Repeat("x", 48))...)
	}
	benchScan(b, indent, func(src []byte) { sinkInt = ConsumeWhitespace(src) })
}

func BenchmarkConsumeSimpleString(b *testing.B) {
	quoted := func(n int) []byte { return append([]byte{'"'}, plainRun(n)...) }
	benchScan(b, quoted, func(src []byte) { sinkInt = ConsumeSimpleString(src) })
}

// stringShapes are the three shapes of string content that behave differently:
// plain ASCII stays on the fast path for its whole length, escaped text leaves
// it every couple of bytes, and non-ASCII text leaves it on nearly every byte.
var stringShapes = [...]struct {
	name string
	make func(n int) []byte
}{
	{"ASCII", func(n int) []byte { return bytes.Repeat([]byte("a"), n) }},
	{"Escaped", func(n int) []byte { return []byte(strings.Repeat(`a"`, n/2+1)[:n]) }},
	{"Unicode", func(n int) []byte { return []byte(strings.Repeat("é", n/2+1)[:n&^1]) }},
}

func BenchmarkAppendQuote(b *testing.B) {
	var flags jsonflags.Flags
	for _, shape := range stringShapes {
		b.Run("shape="+shape.name, func(b *testing.B) {
			// dst is reused across iterations so that what is measured is the
			// scan and the escaping, not the allocator.
			var dst []byte
			benchScan(b, shape.make, func(src []byte) {
				dst, _ = AppendQuote(dst[:0], src, &flags)
			})
		})
	}
}

func BenchmarkNeedEscape(b *testing.B) {
	for _, shape := range stringShapes {
		b.Run("shape="+shape.name, func(b *testing.B) {
			benchScan(b, shape.make, func(src []byte) { sinkBool = NeedEscape(src) })
		})
	}
}

func BenchmarkConsumeString(b *testing.B) {
	for _, shape := range stringShapes {
		b.Run("shape="+shape.name, func(b *testing.B) {
			quoted := func(n int) []byte {
				var flags jsonflags.Flags
				src, err := AppendQuote(nil, shape.make(n), &flags)
				if err != nil {
					panic(err)
				}
				return append(src, bytes.Repeat([]byte(" "), 64)...)
			}
			benchScan(b, quoted, func(src []byte) {
				var flags ValueFlags
				sinkInt, _ = ConsumeString(&flags, src, true)
			})
		})
	}
}
