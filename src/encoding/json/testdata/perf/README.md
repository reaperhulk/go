# JSON performance experiments

Build with the fork's toolchain, including the SIMD load escape-analysis fix:

```
GOEXPERIMENT=simd go build -o /tmp/json-perf encoding/json/testdata/perf
```

The harness uses `encoding/json` with JSON v2 enabled. It includes short fields,
ASCII lengths, repeated ASCII runs separated by escapes or Unicode, and the
existing typed `jsontest` corpus. The `UTF8Short`, `UTF8Greek`, `UTF8CJK`,
`UTF8Emoji`, and `UTF8Mixed` cases exercise dense multibyte text, including Arabic.
Select them with `--cases`. `StringEscaped` and `StringUnicode` represent the same
values with different wire escaping, so their Marshal workloads are identical.
Unmarshal reuses its destination. Both operations
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
and allocations. On Linux they additionally report process CPU nanoseconds per
operation using `getrusage`, including user/system time across runtime threads
and GC work. This excludes time waiting to be scheduled but does not eliminate
CPU frequency, cache, or virtualization noise; retain wall-time results too. The runner reverses binary order on alternate repetitions.
Use a fresh output directory for each run. Record the CPU, toolchain version,
experiment flags, source revisions, and commands with the results. Report
inconclusive time differences honestly; instruction reductions alone are not
proof of proportional speedups on real CPUs.

Method references: [Callgrind collection controls](https://valgrind.org/docs/manual/cl-manual.html)
and [Go diagnostics](https://go.dev/doc/diagnostics).

## UTF-8 validation experiment

The experimental AVX2 validator classifies 32 bytes at a time and checks required
continuation positions with bit masks. It rejects overlong encodings, surrogates,
and values above U+10FFFF. It returns only complete runes and stops before JSON,
HTML, or JavaScript escaping. Invalid input and tails retain scalar error handling.
All loads are within the source slice; no padding or unsafe pointers are required.

See [simdjson's UTF-8 validator](https://github.com/simdjson/simdjson/blob/master/src/generic/stage1/utf8_lookup4_algorithm.h)
and [Keiser and Lemire's UTF-8 validation paper](https://arxiv.org/abs/2010.03090)
for the motivation for block validation. This prototype uses explicit lead and
continuation masks. The optimized path currently targets experimental amd64 AVX2;
AVX-512-capable CPUs use 64-byte UTF-8 blocks, with AVX2 handling 32-byte tails.
Other configurations retain their scalar paths.

## Object-array workloads

`Records` is a compact array of 256 ASCII records with short names, email,
booleans, integers, and tags (about 29 KB). `RecordsPretty` indents the same
values. `StringEnums` is a 22 KB array of 256 records containing repeated
8-byte role, status, plan, and region values. These exercise short fields
inside complete documents rather than standalone JSON strings.

Pass `--fresh` to the runner to allocate a new destination for each Unmarshal.
This includes reflection/destination creation in the measured operation and
reports `UnmarshalFresh`. The default reuses the destination. Both modes still
warm the operation and use the same decoder pooling behavior.
