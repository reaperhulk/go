// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && goexperiment.simd && amd64

package jsonwire

import (
	"bytes"
	"fmt"
	"math/bits"
	"simd"
	"simd/archsimd"
	"strings"
	"testing"
)

func asciiScalar(b []byte) (n int) {
	for n < len(b) && b[n] < 128 && escapeASCII[b[n]] == 0 {
		n++
	}
	return n
}

func asciiAVX2(b []byte) int {
	n := consumeASCIIPrefixAVX2(b)
	return n + asciiScalar(b[n:])
}

func asciiAVX512(b []byte) (n int) {
	space := archsimd.BroadcastInt8x64(32)
	quote := archsimd.BroadcastUint8x64('"')
	slash := archsimd.BroadcastUint8x64('\\')
	amp := archsimd.BroadcastUint8x64('&')
	two := archsimd.BroadcastUint8x64(2)
	gt := archsimd.BroadcastUint8x64('>')
	for len(b)-n >= 64 {
		v := archsimd.LoadUint8x64(b[n:])
		bad := space.Greater(v.BitsToInt8()).Or(v.Equal(quote)).Or(v.Equal(slash)).Or(v.Equal(amp)).Or(v.Or(two).Equal(gt)).ToBits()
		if bad != 0 {
			n += bits.TrailingZeros64(bad)
			break
		}
		n += 64
	}
	archsimd.ClearAVXUpperBits()
	return n + asciiScalar(b[n:])
}

func asciiPortable(b []byte) (n int) {
	space := simd.BroadcastInt8s(32)
	quote := simd.BroadcastUint8s('"')
	slash := simd.BroadcastUint8s('\\')
	amp := simd.BroadcastUint8s('&')
	two := simd.BroadcastUint8s(2)
	gt := simd.BroadcastUint8s('>')
	width := quote.Len()
	var words [8]uint64
	for len(b)-n >= width {
		v := simd.LoadUint8s(b[n:])
		bad := space.Greater(v.BitsToInt8()).Or(v.Equal(quote)).Or(v.Equal(slash)).Or(v.Equal(amp)).Or(v.Or(two).Equal(gt))
		bad.ToInt8s().ToBits().ReshapeToUint64s().Store(words[:])
		var any uint64
		for _, word := range words[:width/8] {
			any |= word
		}
		if any != 0 {
			break
		}
		n += width
	}
	if archsimd.X86.AVX() {
		archsimd.ClearAVXUpperBits()
	}
	return n + asciiScalar(b[n:])
}

func BenchmarkASCIIPrefix(b *testing.B) {
	for _, size := range []int{8, 32, 64, 256, 4096} {
		data := []byte(strings.Repeat("a", size))
		for _, tt := range []struct {
			name    string
			fn      func([]byte) int
			enabled bool
		}{
			{"Scalar", asciiScalar, true}, {"Portable", asciiPortable, true},
			{"AVX2", asciiAVX2, archsimd.X86.AVX2()}, {"AVX512", asciiAVX512, archsimd.X86.AVX512()},
		} {
			if !tt.enabled {
				continue
			}
			b.Run(fmt.Sprintf("%d/%s", size, tt.name), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for b.Loop() {
					if tt.fn(data) != size {
						b.Fatal("incorrect prefix")
					}
				}
			})
		}
	}
}

func TestASCIIPrefixKernels(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fn      func([]byte) int
		enabled bool
	}{
		{"Portable", asciiPortable, true},
		{"AVX2", asciiAVX2, archsimd.X86.AVX2()},
		{"AVX512", asciiAVX512, archsimd.X86.AVX512()},
	} {
		if !tt.enabled {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			for _, size := range []int{0, 1, 31, 32, 33, 63, 64, 65, 129} {
				data := bytes.Repeat([]byte{'a'}, size)
				if got := tt.fn(data); got != size {
					t.Fatalf("size=%d: got %d", size, got)
				}
				for pos := range data {
					for c := 0; c < 256; c++ {
						data[pos] = byte(c)
						if got, want := tt.fn(data), asciiScalar(data); got != want {
							t.Fatalf("size=%d pos=%d byte=%02x: got %d, want %d", size, pos, c, got, want)
						}
					}
					data[pos] = 'a'
				}
			}
		})
	}
}
