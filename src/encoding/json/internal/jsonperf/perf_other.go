// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2 && !linux

package jsonperf

import "errors"

type counters struct{}

func open() (*counters, error) {
	return nil, errors.New("CPU performance counters need Linux")
}

func (c *counters) close()                                       {}
func (c *counters) start()                                       {}
func (c *counters) stop() (instructions, cycles uint64, ok bool) { return 0, 0, false }
