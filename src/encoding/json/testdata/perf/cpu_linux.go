// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "syscall"

// processCPUTime includes user and system CPU time across all runtime threads,
// including GC work, while excluding time waiting to be scheduled.
func processCPUTime() int64 {
	var usage syscall.Rusage
	check(syscall.Getrusage(syscall.RUSAGE_SELF, &usage))
	return usage.Utime.Nano() + usage.Stime.Nano()
}
