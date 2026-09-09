# JSON performance experiments, second round

Two changes are retained: an eight-byte string-cache fix and AVX-512 UTF-8
validation. A whitespace bit-set experiment was rejected after native timing
regressions. These results are incremental to the [previous round](../2026-09-09).

## String-cache fix

The cache previously XORed hashes of the first and last eight bytes. For every
string of exactly eight bytes, those inputs are identical and the hash is zero.
Different eight-byte values therefore repeatedly evict one another from slot
zero. The fix sends this length through the existing four-byte prefix/suffix
case, hashing its two halves once. It changes one comparison and adds no cache
capacity. It applies with or without the SIMD experiment.

This was measured in a **22,163-byte JSON array of 256 records**, with repeated
role, status, plan, and region values. It is not a standalone eight-byte string.

| Destination policy | Before | After | Time change | Allocations/op |
|---|---:|---:|---:|---:|
| Reused | 215.6 us | 174.4 us | -19.11%, p < 0.001 | 1,024 to 0 |
| Fresh | 255.5 us | 196.5 us | -23.11%, p=0.029 | 1,034 to 10 |

Fresh-destination allocation volume drops by 8 KiB/op, from about 50.15 KiB
to 42.15 KiB. A separate earlier run, with the rejected whitespace variant
present equally in both binaries, measured an 8.12% fresh-destination time
reduction. Both runs are retained; the magnitude varies on this shared host.
The cache regression test fails on the original code with eight allocations per
pass through eight repeated values, and passes after the fix.

The selected-code run did not establish time changes on Records, Small,
standalone ASCII8, GolangSource, or TwitterStatus. The earlier cache run reported
GolangSource -4.57%, but instruction counts were essentially unchanged and the
selected-code run was inconclusive, so that is not claimed as a robust benefit.

## AVX-512 UTF-8 validation

On CPUs with the required AVX-512 features, the validator checks 64 bytes per
block. The last 32–63 bytes use AVX2; smaller tails and error reporting remain
scalar. It separately detects incomplete sequences whose continuation bits
would overflow the 64-bit mask. ASCII still uses its existing scanner.

Dispatch is inside the vector backend, keeping the outer UTF-8 length/first-byte
guard inlinable. Unsupported CPUs use AVX2 or the existing scalar fallback.

The selected-code comparison is against the cache-fixed AVX2 implementation:

| Workload | Marshal time change | Unmarshal time change |
|---|---:|---:|
| Greek | -21.35% | -23.31% |
| CJK | -30.67% | -28.29% |
| Emoji | -25.43% | -24.37% |
| Mixed ASCII/Unicode | -27.39% | -28.33% |
| StringUnicode corpus | -17.38% | -14.21% |

The four dense synthetic cases have p <= 0.001 in both operations; StringUnicode
has p=0.015 for Marshal and p=0.043 for Unmarshal. Allocation counts are unchanged
by the vector change. Records, Small, and ASCII4096 showed no significant native
time change in this comparison. Nonsignificance does not establish equivalence.

Callgrind on this host exposes the AVX2 fallback, not the AVX-512 path. Its
fallback measurements differ by less than 0.6% across the checked Unicode cases;
they do **not** measure AVX-512 instruction savings. Native CPU profiling verifies
execution of `consumeUTF8PrefixAVX512`. AVX-512 speed claims use repeated native
wall and process CPU measurements, not extrapolation from instruction counts.

## Rejected whitespace experiment

Profiles identified whitespace scanning as about 11% of samples in TwitterStatus
Unmarshal and 6% in GolangSource. A bounded bit-set classifier reduced executed
instructions by 0.54–3.19% on the tested object/corpus cases, but the second native
run reported Small +5.73% and RecordsPretty +4.40%. It was removed. The patch and
both native runs are included so the instruction/time disagreement is reviewable.

## Method and evidence

Same compiler and host as the previous round: the fork's already-built
`go1.28-devel_8f35ae7c` toolchain, JSON v2, `GOEXPERIMENT=simd`, Intel Xeon
Platinum 8573C, Linux, shared environment. The compiler was not rebuilt between
JSON variants. Native runs use `GOMAXPROCS=1`, normal GC, ten alternating pairs,
and 300ms per benchmark sample. Some samples have substantial scheduling/cache/
virtualization noise; all samples are preserved and geomeans are not presented
as general JSON speedups.

The harness now also reports Linux process CPU time using `getrusage`, summed
across runtime threads including GC. It excludes waiting to be scheduled but
does not remove CPU-frequency, cache, or virtualization effects. The CPU-time
interval also includes the benchmark loop's timer bookkeeping. Both CPU time
and wall time remain in the raw files and benchstat output.

`Records` is a compact array of 256 ASCII records, approximately 29 KB, with
short names, email, numbers, booleans, and tags. `RecordsPretty` indents the same
values. `StringEnums` targets the demonstrated eight-byte cache collision.
`--fresh` includes creating a new destination in each measured Unmarshal;
the default reuses its destination. Fixture construction is excluded, operations
are warmed 20 times, and final output is checked against the expected JSON.

Callgrind uses explicit collection controls, three samples, `GOMAXPROCS=1`,
`GOGC=off`, and asynchronous preemption disabled. Loop counts are retained in
the CSV. The instruction comparisons were collected with the rejected whitespace
change present equally in both arms of the cache and AVX-512-fallback comparisons.
The selected native comparisons were rebuilt without it, matching the retained
source. The rejected patch allows those earlier controls to be reconstructed.

| Directory | Comparison |
|---|---|
| selected-cache-native | `base-cpu` to `cache-selected`, reused destinations |
| selected-cache-fresh | `base-cpu` to `cache-selected`, fresh destinations |
| selected-final-native | `cache-selected` to `final-selected`, AVX2 to AVX-512 |
| cache-native / cache-fresh | Earlier cache comparison with whitespace experiment in both arms |
| final-native | Earlier AVX-512 comparison with whitespace experiment in both arms |
| whitespace-native / whitespace-cpu-native | Initial and second native runs of rejected whitespace change |

[Instruction samples](instruction-samples.csv), [summary](instruction-summary.csv),
[binary checksums](binaries.csv), and [profiles](profiles) preserve the evidence.
The raw native files also contain bytes/op, allocations/op, and process CPU
measurements. See the [harness instructions](../../README.md) for reproduction;
use the same compiled toolchain, restore each JSON source revision, and keep
the harness unchanged in every binary.

## Validation

The full JSON test tree passes with SIMD, with AVX-512 disabled, with AVX2
disabled, and without SIMD. Directed tests cover every valid non-ASCII rune,
malformed sequences, truncation, all byte positions through the second 64-byte
boundary, and JSON/HTML/JavaScript escaping at vector boundaries. Prefix and
quote/stream equivalence fuzzing exercise the selected code. The harness also
cross-compiles for linux/arm64 and js/wasm. [Logs](validation) include the expected
failure of the cache regression test on the original implementation.

## Review and source revisions

- [Benchmark harness](https://github.com/reaperhulk/go/commit/8f4d95cebc45c958267679e513ce6405c5a99d50)
- [String-cache fix](https://github.com/reaperhulk/go/commit/b1c6a52981294012a93cefac2b517adadda95961)
- [AVX-512 validator](https://github.com/reaperhulk/go/commit/210c8a3a9005a8627111468d0a42b2c5d1d7ed68)

`base-cpu` uses JSON sources from the previous tip, `f0e129aa`, and the updated
harness. `cache-selected` uses the cache-fix sources above; `final-selected`
uses both retained changes. The `before` and `whitespace` binaries use the
initial Records-only harness; comparisons within each pair keep their harness
identical. `whitespace-cpu` adds the rejected patch to `base-cpu`, `cache-cpu`
adds the cache fix to that, and `final` adds the selected AVX-512 implementation
to `cache-cpu`. The rejected patch is not on the branch. It contains only the production change;
apply it with `git apply --unidiff-zero` when reproducing the earlier controls.

The selected fuzz runs passed 48,656 prefix checks and 114,982 quote/stream
checks. Native profiling attributes 68.69% of flat samples to the AVX-512
validator in the selected CJK decode run, confirming that the new path executes.
This profile share is a remaining-cost diagnostic, not a speedup percentage.
