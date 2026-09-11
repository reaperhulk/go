// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && linux

package jsonperf

import (
	"runtime"
	"syscall"
	"testing"
)

// TestPlumbing exercises the perf_event_open, ioctl and read sequence with a
// software event, which every Linux kernel provides. Hardware events need a
// PMU that many virtual machines do not expose, so testing the mechanism
// against a hardware event would skip exactly where it is most useful to know
// that the mechanism itself is sound.
func TestPlumbing(t *testing.T) {
	const (
		typeSoftware   = 1 // PERF_TYPE_SOFTWARE
		countSWTaskClk = 1 // PERF_COUNT_SW_TASK_CLOCK
	)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	saved := typeHardwareForTest
	typeHardwareForTest = typeSoftware
	defer func() { typeHardwareForTest = saved }()

	fd, err := openEvent(countSWTaskClk)
	if err != nil {
		if err == syscall.EACCES || err == syscall.EPERM {
			t.Skip("perf_event_open denied; see /proc/sys/kernel/perf_event_paranoid")
		}
		t.Fatalf("openEvent: %v", err)
	}
	c := &counters{instructions: fd, cycles: -1}
	defer c.close()

	c.start()
	spin()
	got, _, ok := c.stop()
	if !ok {
		t.Fatal("stop reported no reading")
	}
	if got == 0 {
		t.Error("counter did not advance across a busy loop")
	}
}

// TestAvailable does not assert that counters are available, because plenty of
// machines cannot provide them; it records what happened so that a benchmark
// run without instr/op metrics has an explanation in the log.
func TestAvailable(t *testing.T) {
	if Available() {
		t.Log("hardware performance counters are available")
		return
	}
	t.Log("hardware performance counters are unavailable:", Reason())
}

var sink uint64

func spin() {
	for i := range uint64(5_000_000) {
		sink += i
	}
}
