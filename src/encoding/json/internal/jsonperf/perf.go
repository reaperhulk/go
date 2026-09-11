// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

// Package jsonperf reports CPU performance counters as benchmark metrics.
//
// Wall-clock benchmarks answer whether a change is faster on the machine that
// ran them. Retired-instruction counts answer something narrower but far more
// stable: how much work the CPU was asked to do. On a shared or virtualized
// host, where ns/op can swing by tens of percent between runs, an instruction
// count usually moves by well under a percent for the same binary and input,
// which makes it the more trustworthy signal for a change whose whole point is
// to do the same work in fewer instructions.
//
// Use it from a benchmark by wrapping the loop:
//
//	func BenchmarkThing(b *testing.B) {
//		jsonperf.Measure(b, len(input), func() {
//			for b.Loop() {
//				sink = thing(input)
//			}
//		})
//	}
//
// When counters are available this reports instructions/op, cycles/op,
// instructions/cycle and, if a positive byte count is given, instructions/byte
// alongside the usual metrics; the names are ordinary [testing.B.ReportMetric]
// units, so benchstat compares them like any other. When counters are not
// available it simply runs the loop, so benchmarks that use it stay portable.
//
// The counts include whatever the benchmark loop itself costs, so compare like
// with like: the same benchmark, over the same input, between two builds.
//
// # Availability
//
// Counters come from perf_event_open(2), so this needs Linux, and needs the
// kernel to expose a hardware PMU to the process. A virtual machine whose host
// does not virtualize the PMU has no hardware events at all, and
// /proc/sys/kernel/perf_event_paranoid above 2 denies them to unprivileged
// processes. [Available] reports whether counters were obtained, and [Reason]
// says why not. Neither condition is an error: the benchmark still runs, it
// just reports time and throughput only.
package jsonperf

import "testing"

// Measure runs fn, which is expected to contain the benchmark loop, with CPU
// performance counters enabled around it, and reports what they saw as
// benchmark metrics. If bytesPerOp is positive it is also used to report
// instructions/byte.
//
// If counters are unavailable, Measure runs fn and reports nothing extra.
func Measure(b *testing.B, bytesPerOp int, fn func()) {
	b.Helper()
	c, err := open()
	if err != nil {
		fn()
		return
	}
	defer c.close()

	c.start()
	fn()
	instructions, cycles, ok := c.stop()
	if !ok || b.N == 0 {
		return
	}
	n := float64(b.N)
	b.ReportMetric(float64(instructions)/n, "instr/op")
	b.ReportMetric(float64(cycles)/n, "cycles/op")
	if cycles > 0 {
		b.ReportMetric(float64(instructions)/float64(cycles), "instr/cycle")
	}
	if bytesPerOp > 0 {
		b.ReportMetric(float64(instructions)/(n*float64(bytesPerOp)), "instr/byte")
	}
}

// Available reports whether CPU performance counters can be read here.
func Available() bool {
	c, err := open()
	if err != nil {
		return false
	}
	c.close()
	return true
}

// Reason returns a short explanation of why [Available] is false, or the empty
// string if counters are available.
func Reason() string {
	c, err := open()
	if err != nil {
		return err.Error()
	}
	c.close()
	return ""
}
