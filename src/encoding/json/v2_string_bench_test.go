// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package json

import (
	"strconv"
	"strings"
	"testing"
)

func BenchmarkStringSize(b *testing.B) {
	for _, size := range []int{8, 32, 256, 4096} {
		s := strings.Repeat("a", size)
		data := []byte(`"` + s + `"`)
		b.Run("Unmarshal/"+strconv.Itoa(size), func(b *testing.B) {
			var dst string
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := Unmarshal(data, &dst); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("Marshal/"+strconv.Itoa(size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := Marshal(s); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
