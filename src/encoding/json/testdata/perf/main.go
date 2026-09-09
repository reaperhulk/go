// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

// This program measures JSON operations after fixture loading and warm-up.
// See README.md for native and Callgrind commands.
package main

import (
	"bytes"
	"encoding/json"
	"encoding/json/internal/jsontest"
	"flag"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
)

var sink []byte

//go:noinline
func measure(n int, op func()) {
	for range n {
		op()
	}
}

func main() {
	name := flag.String("case", "Small", "Small, ASCII8/32/256/4096, EscapeRuns, UnicodeRuns, or a jsontest fixture name")
	action := flag.String("op", "Unmarshal", "Marshal or Unmarshal")
	n := flag.Int("n", 100, "iterations inside main.measure")
	native := flag.Bool("native", false, "use testing.Benchmark instead of fixed iterations")
	profile := flag.String("cpuprofile", "", "native CPU profile output")
	testing.Init()
	flag.Parse()
	if *n < 1 {
		panic("n must be positive")
	}
	data, value := fixture(*name)
	dst := reflect.New(reflect.TypeOf(value).Elem()).Interface()
	want, err := json.Marshal(value)
	check(err)
	var op func()
	switch *action {
	case "Marshal":
		op = func() {
			var err error
			sink, err = json.Marshal(value)
			check(err)
		}
	case "Unmarshal":
		op = func() { check(json.Unmarshal(data, dst)) }
	default:
		panic("unknown operation")
	}
	// Keep Callgrind collection on one OS thread and exclude lazy caches,
	// fixture construction, destination growth, and the explicit GC below.
	runtime.LockOSThread()
	for range 20 {
		op()
	}
	runtime.GC()
	if *profile != "" {
		f, err := os.Create(*profile)
		check(err)
		check(pprof.StartCPUProfile(f))
		defer f.Close()
		defer pprof.StopCPUProfile()
	}
	if *native {
		r := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				op()
			}
		})
		fmt.Printf("BenchmarkJSON/%s/%s %s\t%s\n", *name, *action, r.String(), r.MemString())
	} else {
		callgrind(0x43540004) // START_INSTRUMENTATION
		measure(*n, op)
		callgrind(0x43540005) // STOP_INSTRUMENTATION
		fmt.Printf("%s/%s: %d operations, %d input bytes\n", *name, *action, *n, len(data))
	}
	if *action == "Unmarshal" {
		sink, err = json.Marshal(dst)
		check(err)
	}
	if !bytes.Equal(sink, want) {
		panic("incorrect result")
	}
}

func fixture(name string) ([]byte, any) {
	var value any
	switch {
	case strings.HasPrefix(name, "ASCII"):
		n, err := strconv.Atoi(strings.TrimPrefix(name, "ASCII"))
		check(err)
		s := strings.Repeat("a", n)
		value = &s
	case name == "EscapeRuns" || name == "UnicodeRuns":
		separator := "\n"
		if name == "UnicodeRuns" {
			separator = "世"
		}
		s := strings.Repeat(strings.Repeat("a", 256)+separator, 16)
		value = &s
	case name == "Small":
		value = &struct {
			ID     int      `json:"id"`
			Name   string   `json:"name"`
			Active bool     `json:"active"`
			Tags   []string `json:"tags"`
		}{42, "gopher", true, []string{"go", "json", "simd"}}
	default:
		for _, entry := range jsontest.Data {
			if entry.Name == name && entry.New != nil {
				data, value := entry.Data(), entry.New()
				check(json.Unmarshal(data, value))
				return data, value
			}
		}
		panic("unknown fixture: " + name)
	}
	data, err := json.Marshal(value)
	check(err)
	return data, value
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
