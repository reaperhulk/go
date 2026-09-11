// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && !(goexperiment.simd && amd64)

package jsonwire

// utf8SIMD reports whether this build can validate UTF-8 a vector at a time.
const utf8SIMD = false

func skipUTF8Long(t *scanTables, b []byte, n int) int { return n }
