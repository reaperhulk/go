// asmcheck

// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen

//go:noinline
func target(x int) int { return x + 1 }

func via(x int, f func(int) int) int { return f(x) }

// A func parameter bound to a declared function after inlining is
// called directly, not through a closure register.
func DirectAfterInline(x int) int {
	// amd64:`CALL command-line-arguments\.target\(SB\)` -`CALL [A-Z]`
	// arm64:`CALL command-line-arguments\.target\(SB\)` -`CALL \(R[0-9]+\)`
	return via(x, target)
}
