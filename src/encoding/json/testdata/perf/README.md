# JSON performance experiments

Build with the fork's toolchain, including the SIMD load escape-analysis fix:

```
GOEXPERIMENT=simd go build -o /tmp/json-perf encoding/json/testdata/perf
```

The harness uses `encoding/json` with JSON v2 enabled. It includes short fields,
ASCII lengths, repeated ASCII runs separated by escapes or Unicode, and the
existing typed `jsontest` corpus. Unmarshal reuses its destination. Both operations
are warmed for 20 iterations and checked against an expected result. This is a
steady-state benchmark; cold caches and fresh destinations are separate workloads.

For each revision, build a separate executable **using the same compiler**.
Do not compare different toolchains when attributing results to JSON changes.
Keep the harness identical in all builds, including the scalar control.

## Instruction counts

```
python3 src/encoding/json/testdata/perf/run.py \
  --binary before=/tmp/json-before --binary after=/tmp/json-after \
  --valgrind /path/to/valgrind --repeat 3 --out /tmp/json-counts
```

The runner sets `GOMAXPROCS=1`, disables asynchronous preemption and GC, and
starts and stops instrumentation explicitly around the non-inlined `main.measure`
loop. The amd64 client-request stub is a no-op on native hardware. Function-entry
toggling is intentionally avoided: Go stack moves can confuse Callgrind's call-stack
tracking.
Setup, warm-up, the explicit GC, result checking, and printing are excluded.
Small cases execute 200 operations; large corpus cases execute 3. The loop and
indirect operation call are included equally in all versions. Raw Callgrind
profiles, stdout, stderr, and per-operation counts are preserved. Check that
SIMD profiles actually contain `consumeASCIIPrefixAVX2`: unsupported CPUs or
Valgrind versions may take the fallback or reject an instruction.

`Ir` counts executed guest instructions, not cycles or native elapsed time.
An AVX2 operation and a scalar operation each count as one instruction. Branch
misses are simulated, not hardware counters. GC is disabled only for these
counts, so they do not establish the cost of collection under allocation load.
Repeat counts, check their spread, and use `--scale 2` to check linearity; map hash seeds can vary instruction paths.

## Native time, allocations, and profiles

```
python3 src/encoding/json/testdata/perf/run.py \
  --binary before=/tmp/json-before --binary after=/tmp/json-after \
  --repeat 10 --benchtime 200ms --out /tmp/json-native
benchstat /tmp/json-native/before.txt /tmp/json-native/after.txt
GOMAXPROCS=1 /tmp/json-after -case GolangSource -op Unmarshal \
  -native -test.benchtime=10s -cpuprofile=/tmp/json-cpu.pprof
go tool pprof -top /tmp/json-after /tmp/json-cpu.pprof
```

Native runs use `testing.Benchmark`/`B.Loop`, normal GC, and report time, bytes,
and allocations. The runner reverses binary order on alternate repetitions.
Use a fresh output directory for each run. Record the CPU, toolchain version,
experiment flags, source revisions, and commands with the results. Report
inconclusive time differences honestly; instruction reductions alone are not
proof of proportional speedups on real CPUs.

Method references: [Callgrind collection controls](https://valgrind.org/docs/manual/cl-manual.html)
and [Go diagnostics](https://go.dev/doc/diagnostics).
