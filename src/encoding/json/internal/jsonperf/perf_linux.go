// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && linux

package jsonperf

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

// eventAttr mirrors struct perf_event_attr from <linux/perf_event.h>. Only the
// leading fields and the option bitfield are used; the rest is present so that
// the size the kernel is told matches the struct it is handed.
type eventAttr struct {
	typ            uint32
	size           uint32
	config         uint64
	sampleInterval uint64
	sampleType     uint64
	readFormat     uint64
	options        uint64
	wakeupEvents   uint32
	bpType         uint32
	config1        uint64
	config2        uint64
	branchSample   uint64
	regsUser       uint64
	stackUser      uint32
	clockID        int32
	regsIntr       uint64
	auxWatermark   uint32
	sampleMaxStack uint16
	_              uint16
	auxSampleSize  uint32
	_              uint32
	sigData        uint64
	config3        uint64
}

const (
	typeHardware      = 0 // PERF_TYPE_HARDWARE
	countHWCPUCycles  = 0 // PERF_COUNT_HW_CPU_CYCLES
	countHWInstrs     = 1 // PERF_COUNT_HW_INSTRUCTIONS
	optDisabled       = 1 << 0
	optExcludeKernel  = 1 << 5
	optExcludeHV      = 1 << 6
	ioctlEnable       = 0x2400 // PERF_EVENT_IOC_ENABLE
	ioctlDisable      = 0x2401 // PERF_EVENT_IOC_DISABLE
	ioctlReset        = 0x2403 // PERF_EVENT_IOC_RESET
	currentThreadOnly = 0      // pid argument: the calling thread
)

// counters holds one open counter per event.
type counters struct {
	instructions int
	cycles       int
	locked       bool
}

// typeHardwareForTest is the event type that [openEvent] asks for. It is a
// variable only so that TestPlumbing can exercise this code against a software
// event on machines that expose no hardware PMU.
var typeHardwareForTest uint32 = typeHardware

func openEvent(config uint64) (int, error) {
	attr := eventAttr{
		typ:     typeHardwareForTest,
		size:    uint32(unsafe.Sizeof(eventAttr{})),
		config:  config,
		options: optDisabled | optExcludeKernel | optExcludeHV,
	}
	const (
		anyCPU  = ^uintptr(0) // cpu argument: -1, any CPU
		noGroup = ^uintptr(0) // group_fd argument: -1, not in a group
	)
	fd, _, errno := syscall.Syscall6(syscall.SYS_PERF_EVENT_OPEN,
		uintptr(unsafe.Pointer(&attr)), currentThreadOnly, anyCPU, noGroup, 0, 0)
	runtime.KeepAlive(&attr)
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}

func open() (*counters, error) {
	// The counters follow the OS thread, not the goroutine, so the benchmark
	// has to stay put for as long as they are open.
	runtime.LockOSThread()
	c := &counters{instructions: -1, cycles: -1, locked: true}

	instrs, err := openEvent(countHWInstrs)
	if err != nil {
		c.close()
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENODEV) {
			return nil, errors.New("no hardware PMU is exposed to this machine")
		}
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return nil, errors.New("perf_event_open denied; see /proc/sys/kernel/perf_event_paranoid")
		}
		return nil, err
	}
	c.instructions = instrs

	// Cycles are a bonus: instructions alone are still worth reporting.
	if cycles, err := openEvent(countHWCPUCycles); err == nil {
		c.cycles = cycles
	}
	return c, nil
}

func (c *counters) close() {
	for _, fd := range [...]int{c.instructions, c.cycles} {
		if fd >= 0 {
			syscall.Close(fd)
		}
	}
	c.instructions, c.cycles = -1, -1
	if c.locked {
		c.locked = false
		runtime.UnlockOSThread()
	}
}

func (c *counters) ioctl(op uintptr) {
	for _, fd := range [...]int{c.instructions, c.cycles} {
		if fd >= 0 {
			syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), op, 0)
		}
	}
}

func (c *counters) start() {
	c.ioctl(ioctlReset)
	c.ioctl(ioctlEnable)
}

func (c *counters) stop() (instructions, cycles uint64, ok bool) {
	c.ioctl(ioctlDisable)
	instructions, ok = readCount(c.instructions)
	cycles, _ = readCount(c.cycles)
	return instructions, cycles, ok
}

func readCount(fd int) (uint64, bool) {
	if fd < 0 {
		return 0, false
	}
	var buf [8]byte
	if n, err := syscall.Read(fd, buf[:]); n != len(buf) || err != nil {
		return 0, false
	}
	var v uint64
	for i := len(buf) - 1; i >= 0; i-- {
		v = v<<8 | uint64(buf[i])
	}
	return v, true
}
