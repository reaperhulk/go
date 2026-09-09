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
	fresh := flag.Bool("fresh", false, "allocate a fresh Unmarshal destination per operation")
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
		op = func() {
			if *fresh {
				dst = reflect.New(reflect.TypeOf(value).Elem()).Interface()
			}
			check(json.Unmarshal(data, dst))
		}
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
			startCPU := processCPUTime()
			for b.Loop() {
				op()
			}
			if endCPU := processCPUTime(); startCPU >= 0 {
				b.ReportMetric(float64(endCPU-startCPU)/float64(b.N), "cpu-ns/op")
			}
		})
		opName := *action
		if *fresh && *action == "Unmarshal" {
			opName += "Fresh"
		}
		fmt.Printf("BenchmarkJSON/%s/%s %s\t%s\n", *name, opName, r.String(), r.MemString())
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
	case name == "StringEnums":
		type record struct {
			ID     int    `json:"id"`
			Role   string `json:"role"`
			Status string `json:"status"`
			Plan   string `json:"plan"`
			Region string `json:"region"`
		}
		rows := make([]record, 256)
		roles := []string{"customer", "operator"}
		statuses := []string{"approved", "rejected"}
		plans := []string{"business", "personal"}
		regions := []string{"us-west2", "us-east1"}
		for i := range rows {
			rows[i] = record{i, roles[i%2], statuses[i/2%2], plans[i/4%2], regions[i/8%2]}
		}
		value = &rows
	case name == "Records" || name == "RecordsPretty":
		type record struct {
			ID     int      `json:"id"`
			Name   string   `json:"name"`
			Email  string   `json:"email"`
			Active bool     `json:"active"`
			Score  int      `json:"score"`
			Tags   []string `json:"tags"`
		}
		rows := make([]record, 256)
		for i := range rows {
			rows[i] = record{i, "gopher" + strconv.Itoa(i), "gopher@example.com", i%3 != 0, i * 7, []string{"go", "json", "simd"}}
		}
		value = &rows
	case name == "Small":
		value = &struct {
			ID     int      `json:"id"`
			Name   string   `json:"name"`
			Active bool     `json:"active"`
			Tags   []string `json:"tags"`
		}{42, "gopher", true, []string{"go", "json", "simd"}}
	case strings.HasPrefix(name, "UTF8"):
		texts := map[string]string{
			"UTF8Short": "世界😀",
			"UTF8Greek": strings.Repeat("Καλημέρα κόσμε ", 256),
			"UTF8CJK":   strings.Repeat("你好世界こんにちは世界", 256),
			"UTF8Emoji": strings.Repeat("😀🌍🚀🎉", 256),
			"UTF8Mixed": strings.Repeat("Hello 世界 مرحبا κόσμε 😀 ", 256),
		}
		s, ok := texts[name]
		if !ok {
			panic("unknown fixture: " + name)
		}
		value = &s
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
	if name == "RecordsPretty" {
		var pretty bytes.Buffer
		check(json.Indent(&pretty, data, "", "  "))
		data = pretty.Bytes()
	}
	return data, value
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
