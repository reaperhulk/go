# JSON SIMD measurements, 2026-09-09

The retained changes accelerate ASCII runs separated by escapes or Unicode,
and validate dense UTF-8 in AVX2 blocks. Each integration is a separate commit.
Allocation counts are unchanged. Short ASCII decoding remains a weakness of the
original experimental SIMD integration; this series does not eliminate it.

## Native results

Ten alternating before/after samples per workload, using Go's native benchmark
loop and normal GC. Percentages below compare the median elapsed time; negative
means faster. These are measurements on this host, not portable guarantees.

| Change versus immediate parent | Workload | Time change |
|---|---|---:|
| Scan each ASCII run while quoting | EscapeRuns | -71.93% |
| Scan each ASCII run while quoting | UnicodeRuns | -71.08% |
| Scan ASCII runs during validation | EscapeRuns | -58.06% |
| Scan ASCII runs during validation | UnicodeRuns | -76.60% |
| Scan ASCII runs while unescaping | EscapeRuns | -55.49% |
| Validate UTF-8 while quoting | Greek / CJK / emoji / mixed | -72.11% / -57.97% / -52.90% / -66.90% |
| Validate UTF-8 during decoding | Greek / CJK / emoji / mixed | -68.21% / -54.42% / -51.08% / -58.81% |
| Validate UTF-8 while quoting | StringUnicode | -50.87% |
| Validate UTF-8 during decoding | StringUnicode | -49.47% |

All changes in that table have p <= 0.002 in the corresponding primary run.
The two ASCII decoding percentages are incremental and must not be added.
StringEscaped and StringUnicode have identical Marshal values; their different
wire representations matter only for Unmarshal. Escaped Unicode still requires
scalar escape processing: StringEscaped Unmarshal showed no significant native
change from the UTF-8 validator.

ASCII remains an explicit gate. The final three-way comparison uses the scalar
JSON control, the original SIMD branch, and the final build, all compiled by the
same compiler:

| ASCII workload | Final Marshal vs scalar | Final Unmarshal vs scalar |
|---|---:|---:|
| Small typed object | inconclusive | inconclusive |
| 8-byte string | inconclusive | +7.99% |
| 32-byte string | -14.45% | -13.10% |
| 256-byte string | -47.70% | -63.53% |
| 4096-byte string | -77.02% | -76.08% |

The 8-byte decode regression is significant (p < 0.001), and is consistent with
the original SIMD scanner making ConsumeSimpleString too large to inline.
Original SIMD instruction counts were +4.93% for ASCII8 Unmarshal and +6.95%
for Small Unmarshal versus scalar. No SIMD block executes for the standalone
8-byte string. A scalar short probe and a word quote probe were rejected: the
former saved about 1.7% of Small instructions but added about 13% for ASCII32;
the latter barely changed Small and added work for longer strings. Their samples
are retained in the instruction CSVs, but their code is not enabled.

## Noise, regressions, and limits

This shared host has substantial native timing variability. A nonsignificant
result does not establish equivalence, and a geomean across these selected
fixtures is not a general JSON speedup. See every raw sample and benchstat result
in the native directories, including allocation and bytes-per-operation results.

The primary ASCII validation run reported an ASCII256 decode regression of
6.07% (p=0.029). An independent 20-pair, 500ms run was inconclusive (p=0.274).
The primary unquote run reported ASCII32 +12.74% and CanadaGeometry +6.65%;
independent 20-pair confirmations were inconclusive (p=0.180 and p=0.640).
Both primary and confirmation samples are included.

The primary UTF-8 encoder run reported Small Marshal +2.53% (p=0.027), alongside
a 0.52% instruction increase. The final ASCII run showed no significant Small
Marshal difference versus either original SIMD or scalar. The same final run
reported ASCII4096 Marshal +9.39% versus original SIMD (p=0.023). See the separate
ASCII confirmation results for an independent check of these two workloads.
That twenty-pair check was inconclusive for Small (552.9 to 559.5 ns, p=0.659)
and ASCII4096 (1.012 to 1.001 us, p=0.723). It does not establish equivalence;
the primary regression samples remain included.
The UTF-8 decoder run's CanadaGeometry time improvement is not attributed to
UTF-8 SIMD: its instruction count is essentially unchanged.

These experiments target amd64 AVX2 under GOEXPERIMENT=simd. Default builds,
AVX2-disabled builds, arm64, and Wasm retain scalar fallbacks. This does not
establish NEON, Wasm SIMD, portable SIMD, cold-start, or multi-core performance.
Native allocation counts are unchanged; some corpus B/op values vary slightly
with pool and GC amortization. No claim of reduced overall memory use is made.

## Instructions and profiles

[ASCII instruction changes](ascii-instructions.md) and
[UTF-8 instruction changes](utf8-instructions.md) compare each change with its
immediate parent. [All instruction samples](instruction-samples.csv) retain
three repetitions, including conditional/indirect branches and simulated misses.
[Summary](instruction-summary.csv) includes medians and extrema.

Valgrind 3.27.1 Callgrind collected only the fixed operation loop using explicit
START/STOP_INSTRUMENTATION client requests. Function-entry toggling was discarded:
Go's movable stacks confused call-stack tracking and produced invalid counts.
The retained data parses the final `totals:` line, not the zero `summary:` line.
Setup, warmup, GC, correctness checks, and printing are outside instrumentation.
The loop and indirect call overhead remain, equally for each binary.

`Ir` counts executed guest instructions, not cycles. AVX2 and scalar instructions
each count as one. Valgrind exposes different CPU features from native execution;
for example, a hash fallback was observed. Simulated branch misses are not
hardware counters. The [linearity check](linearity.csv) doubles the operation
count for Small and GolangSource; normalized counts differ by less than 0.7%
and 0.08%, respectively. Flat counts are useful; Go's movable stacks make
inclusive Callgrind attribution unreliable.

The ASCII unquote change was motivated by AppendUnquote accounting for 72.51%
of flat instructions in EscapeRuns after the validation optimization. Native
[pprof summaries](profiles) confirm the UTF-8 path executes: in StringUnicode
Marshal, scalar decodeRuneSlow falls from 40.49% to 7.37% of samples. These are
profile shares from separate timed runs, not comparable absolute CPU durations.
The validator becomes the largest remaining sampled cost.

## Environment and reproduction

- Intel Xeon Platinum 8573C, amd64 AVX2; Linux 6.18.35, 8-CPU cgroup quota,
  20 GiB memory limit. No affinity pinning; shared host.
- Compiler: `go1.28-devel_8f35ae7c`, built once from this fork, including the
  SIMD load escape-analysis fix. JSON v2 is the default in this fork.
- Every comparison uses that compiler and `GOEXPERIMENT=simd`.
- Callgrind: `GOMAXPROCS=1 GOGC=off GODEBUG=asyncpreemptoff=1`, cache simulation
  off, branch simulation on, 200 operations for small/synthetic cases and 3 for
  corpus cases, 20 warmups followed by GC. Three samples per binary/workload.
- Native: `GOMAXPROCS=1`, normal GC; ten alternating pairs, 200ms for ASCII-run
  comparisons, 300ms for dense UTF-8 and final ASCII. Confirmations use twenty
  pairs at 500ms. Destinations are reused. `testing.Benchmark` uses `B.Loop`.
- CPU profiles: StringUnicode Marshal/Unmarshal before and after UTF-8 changes,
  native `-test.benchtime=3s -cpuprofile=...`; summaries from `go tool pprof -top`.

[Binary checksums and JSON source revisions](binaries.csv) identify builds.
The harness uses the corrected Callgrind client requests in every compared
binary. The later UTF-8 fixtures do not change existing cases. Equivalent source
was built before publishing, so build IDs can contain local commit identifiers.

| Result directory | Labels mapped to binaries in binaries.csv |
|---|---|
| native-encode | current=current; encode=group-encode |
| native-decode, native-confirm | encode=group-encode; final=final |
| native-unquote, native-confirm-unquote | final=final; unquote=unquote |
| native-utf8-encode | before=utf8-before; encode=utf8-encode2 |
| native-utf8-decode | encode=utf8-encode2; decode=utf8-decode2 |
| native-ascii-final | scalar=scalar; original=current; final=utf8-decode2 |
| native-ascii-confirm | original=current; final=utf8-decode2 |

Use the [harness instructions](../../README.md) to repeat either method. Build
historical JSON sources with the same already-built compiler in a dedicated
clean checkout; retain the latest harness. For example:

```sh
git restore --source=<revision> --worktree -- src/encoding/json/internal/jsonwire
GOEXPERIMENT=simd ./bin/go build -o /tmp/json-before encoding/json/testdata/perf
git restore --worktree -- src/encoding/json/internal/jsonwire
GOEXPERIMENT=simd ./bin/go build -o /tmp/json-after encoding/json/testdata/perf
python3 src/encoding/json/testdata/perf/run.py \
  --binary before=/tmp/json-before --binary after=/tmp/json-after \
  --repeat 10 --benchtime 300ms --out /tmp/json-native
benchstat /tmp/json-native/before.txt /tmp/json-native/after.txt
```

The decoder UTF-8 instruction run was interrupted by an environment restart.
Complete raw profiles for the first ten cases were retained; GolangSource and
TwitterStatus were rerun as a separate batch and CanadaGeometry as another.
Every retained case has three complete samples per binary. Incomplete profiles
and the earlier invalid function-toggle measurements are excluded. An interrupted,
unbalanced ASCII confirmation batch was also replaced by a complete twenty-pair
run; the published confirmation contains only that complete run.

## Correctness

[Validation logs](validation) cover the full encoding/json package tree with
SIMD, without SIMD, and with AVX2 disabled, plus simd/archsimd tests. The jsonwire
tests also cross-compile for arm64 and Wasm. The UTF-8 tests check every valid
non-ASCII rune, byte boundaries, truncation, and malformed leading/continuation
classes. Fuzz runs passed: ASCII unquoting 25,412 executions; UTF-8 prefixes
181,532 executions; quote/stream equivalence 166,579 executions. Valid input,
invalid UTF-8 replacement/errors, HTML escaping, JavaScript separators, and
resumable decoding retain scalar semantics.

The UTF-8 validator and its tests are separate from the three-line encoder and
decoder integrations. It returns complete validated runes, leaves partial tails
and detailed errors to scalar code, and never loads beyond the input slice.

## Review units

| Commit | Change |
|---|---|
| [a809b6c4](https://github.com/reaperhulk/go/commit/a809b6c43b75d4f46235e7dc5f69999e0f374536) | Scan each ASCII run while quoting |
| [f45b4cc3](https://github.com/reaperhulk/go/commit/f45b4cc3a312eeb50ff59c90397e2c193d8e0041) | Three-line ASCII validation integration |
| [4bd5dc1d](https://github.com/reaperhulk/go/commit/4bd5dc1df69203004c33a91092c95fb353e796c8) | Three-line ASCII unquote integration |
| [4d22fd4c](https://github.com/reaperhulk/go/commit/4d22fd4c872bbcfc58d99e7f209d6216f011876e) | Standalone AVX2 UTF-8 validator and tests |
| [e9228320](https://github.com/reaperhulk/go/commit/e92283200f7d7c1964467a7d411cefa1a1b221d1) | Three-line UTF-8 quote integration and equivalence fuzzer |
| [bcf196b8](https://github.com/reaperhulk/go/commit/bcf196b8fe94ac4a2a195fddce032c8393423045) | Three-line UTF-8 decoding integration |
