// errorcheck -0 -m

// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test that a call through a function-typed variable that is
// statically bound to a declared function becomes a direct call,
// in particular after inlining a function with a func parameter.

package p

//go:noinline
func target(b []byte) int { // ERROR "b does not escape"
	return len(b)
}

func via(b []byte, f func([]byte) int) int { // ERROR "can inline via" "leaking param: b" "f does not escape"
	return f(b)
}

func Inlined(b []byte) int { // ERROR "can inline Inlined" "b does not escape"
	return via(b, target) // ERROR "inlining call to via" "devirtualizing f to target"
}

func Local(b []byte) int { // ERROR "can inline Local" "b does not escape"
	f := target
	return f(b) // ERROR "devirtualizing f to target"
}

func Reassigned(b []byte, ok bool) int { // ERROR "can inline Reassigned" "leaking param: b"
	f := target
	if ok {
		f = other
	}
	return f(b)
}

//go:noinline
func other(b []byte) int { // ERROR "leaking param: b"
	sink = b
	return 0
}

var sink []byte
