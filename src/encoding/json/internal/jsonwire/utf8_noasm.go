// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && (!goexperiment.simd || !amd64)

package jsonwire

func consumeUTF8Prefix([]byte) int { return 0 }
