// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// callgrind issues a Valgrind client request, a no-op outside Valgrind.
// Explicit requests avoid relying on call-stack tracking across Go stack moves.
func callgrind(request uintptr)
