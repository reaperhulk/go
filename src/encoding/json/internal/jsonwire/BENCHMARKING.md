# Measuring the vectorized JSON scanners

`scan.go` and `scan_simd_amd64.go` add a vectorized implementation of the byte
scans at the heart of JSON decoding and encoding. This describes how to tell
whether that implementation is actually earning its place, and how to avoid the
ways such a measurement usually goes wrong.

Everything here is built out of tooling that already ships with Go: `go test
-bench`, `testing.B.ReportMetric`, `go tool pprof`, `go build -gcflags=-m`, and
`benchstat` from `golang.org/x/perf`.

    go install golang.org/x/perf/cmd/benchstat@latest

## The two implementations

The vectorized scanner exists only under `GOEXPERIMENT=simd`, and only on
platforms that have one (today, amd64 with AVX2 and arm64 with NEON —
`simdHardware` reports whether the running CPU qualifies). Everywhere else the
same code paths run a scalar bulk scanner. That gives two ways to compare, and
they answer different questions:

- **In-process**, by flipping the package-level `useSIMD` variable. Both
  implementations run in one binary, over one set of inputs, with identical
  codegen everywhere else. This isolates the scanner, and it is what the
  `Scalar` and `SIMD` sub-benchmarks in `scan_bench_test.go` do.
- **Between builds**, with and without `GOEXPERIMENT=simd`. This is what a user
  would actually get, including any second-order effects on inlining and code
  layout, and it is the only way to measure the change from outside this
  package. `benchcmp.sh` does this.

Neither answers whether the *restructuring* that the vectorized path required
was free. For that, compare against a commit from before the change:

    git stash && go test -c -o /tmp/base.test encoding/json/v2 && git stash pop

and pass `/tmp/base.test`'s output to `benchstat` alongside the others. A
scalar build that has regressed against that baseline is a regression for every
user who does not set `GOEXPERIMENT=simd`, however well the vectorized build
does.

## Running the benchmarks

Scanner micro-benchmarks, both implementations, every length:

    cd src/encoding/json/internal/jsonwire
    GOEXPERIMENT=simd go test -run=XXX -bench=. -benchtime=50ms -count=6 . > micro.txt
    benchstat -col /mode micro.txt

Whole-package benchmarks, between builds:

    src/encoding/json/internal/jsonwire/benchcmp.sh -n 8

`benchcmp.sh` builds each variant once, then runs them **interleaved**. That
matters more than the run count. A shared or virtualized machine drifts —
frequency, steal time, neighbours — and a hundred runs of A followed by a
hundred of B will report a confident difference that is really just the machine
changing under you. Interleaving spreads that drift across both variants, where
`benchstat`'s statistics can absorb it.

## Choosing inputs

`scanLengths` in `scan_bench_test.go` runs from 0 to 4096 bytes and every
benchmark reports all of it, because the interesting result is not the peak
throughput but the crossover. A vectorized scan of a 4 KiB run is easy to make
look good; the question that decides whether the change is worth having is
what it costs on the 8-byte object name, and real JSON is mostly 8-byte object
names.

`stringShapes` covers the three behaviours that differ: plain ASCII stays on
the fast path for its whole length, escaped text leaves it every couple of
bytes, and non-ASCII text leaves it on nearly every byte. The last two are
where a vectorized scanner is most likely to lose, since it pays setup cost for
a run that ends immediately, so they are benchmarked deliberately rather than
left to chance.

The `BenchmarkTestdata` corpus in `encoding/json/v2` supplies the end-to-end
view over real documents (`CitmCatalog`, `TwitterStatus`, `SyntheaFhir`,
`GolangSource`, `CanadaGeometry`, plus string-heavy `StringEscaped` and
`StringUnicode`).

## Instruction counting

Wall-clock time answers whether a change was faster on the machine that ran it.
Retired-instruction counts answer something narrower but much steadier: how
much work the CPU was asked to do. On a noisy host `ns/op` can move by tens of
percent between runs of the same binary, while the instruction count for the
same binary over the same input moves by a fraction of a percent — which makes
it the better signal for a change whose entire claim is that it does the same
work in fewer instructions.

`encoding/json/internal/jsonperf` wraps `perf_event_open(2)` and reports the
counters as ordinary benchmark metrics, so `benchstat` compares them like any
other column:

    BenchmarkIndexEscapeByte/SIMD/Len=512-4  57.3 ns/op  8938 MB/s  184 instr/op  0.36 instr/byte

It is wired into the scanner benchmarks here and into `BenchmarkTestdata` in
`encoding/json/v2`. When counters are unavailable the benchmarks run exactly as
before and simply report nothing extra, so nothing here depends on having them.

Counters need Linux and a hardware PMU that the kernel will expose. Two things
commonly get in the way, and `jsonperf.Reason` says which:

- A virtual machine whose host does not virtualize the PMU has no hardware
  events at all. `/sys/bus/event_source/devices/` will have no `cpu` entry, and
  `perf_event_open` fails with `ENOENT`. This is the usual case in CI and in
  cloud sandboxes.
- `/proc/sys/kernel/perf_event_paranoid` above 2 denies hardware events to
  unprivileged processes, which shows up as `EACCES`.

`jsonperf`'s own test exercises the `perf_event_open`/`ioctl`/`read` sequence
against a *software* event, which every Linux kernel provides, so a machine
without a PMU still verifies that the mechanism is sound rather than skipping
the only test that would have caught a bug in it.

## What the numbers currently say

The work lands as a chain of CLs, and each was measured on its own, stacked
on the ones before it, so that the benefit of each step is known separately
from the total. All figures are from a 4-vCPU Intel Xeon at 2.80GHz (AVX2),
a shared VM: `-benchtime=100ms -count=10` interleaved across all variants
for the corpus, `-count=8` for Marshal/Unmarshal, `-count=6` for the
scanners. Rows carry 5-15% variance; read geomeans and the rows marked
significant, not every cell. Where a CL's own run and the final run differ
by a few points, that is the machine, not the code.

### The chain, stacked, on the `BenchmarkTestdata` Value benchmarks

Each row is the geomean of one build against the code before any of it,
with the rows that moved significantly in that step.

    CL                                              geomean   what moved (p < 0.05)
    1  window + scalar bulk scan                     -2.3%   Citm/Synthea encode -6/-7%, Synthea/Twitter decode -5/-6%
    2  AVX2 string scanner                           -4.5%   Synthea encode -11%, Twitter decode -6%
    5  whitespace runs in bulk                       -7.9%   Citm -24/-25%, Synthea -16/-15%
    6  SIMD UTF-8 validation                        -24.2%   StringUnicode -68/-70%, Twitter -16/-13%, GolangSource decode -9%
    7  signature filter for duplicate names         -29.0%   Twitter -34/-39%
    8  full-grammar number scan + SWAR digits       -29.2%   Canada decode -19%, encode -12%

    final, GOEXPERIMENT=simd                        -29.2%
    final, GOEXPERIMENT=nosimd                       -5.0%   Canada -12/-19%, Twitter -14/-12%; Citm encode +5%

CL 3 (counters), CL 4 (this document) and CL 9 (the arm64 draft) change no
amd64 code path.

The `nosimd` row is what every user who does not set `GOEXPERIMENT=simd`
gets: the number scan, the signature filter and the class tables are all
portable, and the SWAR whitespace and digit scans run everywhere. It is the
row that must never regress, per benchmark, and it is measured per CL: the
table under "On arm64" below was built from a binary at every commit of the
chain, run interleaved, and the standard for the chain is that no commit
leaves any row above the baseline. These amd64 figures predate the scalar
path fixes folded into the chain after the arm64 measurements (the CL 5 and
CL 7 rows in particular), so the per-CL numbers there are due for a re-run;
the CitmCatalog encode +5% that this row used to carry was one of them.

### On arm64 (Apple M1 Max), per CL, against the code before the chain

A `GOEXPERIMENT=nosimd` binary at every commit, plus `simd` at the last two
(the only ones with an arm64 vector path), all 28 `BenchmarkTestdata` rows,
interleaved, `-benchtime=100ms -count=5`, medians. Percent versus the base
commit; a row is "up" if it is more than 1% above it, which is this
machine's run-to-run band for a row.

    CL                              1     2     3     4     5     6     7     8     9    10   9s   10s
    geomean, 28 rows              -0.1  -1.6  -1.6  -1.5  -2.3  -2.7  -4.2  -5.0  -5.0  -5.0  -6.3  -6.5
    worst row                     +0.4  +1.1  +2.5  +2.1  +0.6  +0.3  -0.0  +0.3  +0.4  +0.2  +0.3  +0.2
    CanadaGeometry Decode/Value   -0.1  -0.1  +0.6  +0.6  -2.3  -3.0  -3.0 -17.4 -17.5 -17.5 -16.3 -16.5
    CitmCatalog    Decode/Value   +0.1  -1.4  -1.0  -1.1  -8.2  -8.2  -7.8  -7.5  -7.3  -7.3  -9.9 -10.8
    SyntheaFhir    Decode/Value   -0.1  -4.0  -4.4  -4.4  -2.6  -2.8  -2.1  -1.6  -1.5  -1.6  -4.5  -4.1
    TwitterStatus  Decode/Value   +0.3  -3.1  -3.4  -3.2  -3.0  -2.8 -15.7 -15.3 -15.1 -15.1 -17.8 -17.6
    StringUnicode  Unmarshal      -0.3  -4.7  -4.6  -4.4  -4.1  -4.4  -4.4  -4.4  -4.4  -4.0  -4.5  -4.6
    CitmCatalog    Unmarshal      +0.4  +1.1  +2.5  +2.1  -0.2  -2.7  -2.8  -2.2  -1.9  -1.8  -8.0  -7.7

CLs 3 and 4 change no arm64 code path, so the spread in the CitmCatalog
Unmarshal row across CLs 2-4 is that row's noise; the +1% it sits at after
CL 2 is the one place in the chain that is not clearly at or below the base,
and it is gone by CL 5. The simd columns are what the NEON port adds on top
of the scalar path; the arm64 UTF-8 validator is not written yet.

### The reflection paths users actually call

`BenchmarkTestdata/*/(Marshal|Unmarshal)/Concrete` go through
`encoding/json/v2`, where reflection, allocation and field matching share
the profile with the scanners, so the totals are smaller:

    final, GOEXPERIMENT=simd    -12.2% geomean
        StringUnicode Unmarshal -61%, Twitter Unmarshal -20%,
        Canada/Citm/Synthea Unmarshal -9/-9/-9%, String* Marshal -14/-16%
    final, GOEXPERIMENT=nosimd   +0.7% geomean (Canada Unmarshal -8%, Citm Unmarshal +7%)

### The scanners themselves (both implementations in one binary)

    IndexStringByte/len=4096                 1545ns  ->   200ns   -87%
    ConsumeSimpleString/len=4096             1539ns  ->   203ns   -87%
    AppendQuote/shape=ASCII/len=512           214ns  ->    48ns   -77%
    ConsumeWhitespace/len=16                 11.7ns  ->   4.1ns   -65%
    ConsumeWhitespace/len=64                 28.2ns  ->   6.8ns   -76%
    ConsumeString/shape=Unicode/len=64        143ns  ->    21ns   -86%
    ConsumeString/shape=Unicode/len=4096     7.8us   ->  0.49us   -94%
    AppendQuote/shape=Unicode/len=4096       8.1us   ->  0.54us   -93%
    AppendQuote/shape=Escaped/*                                unchanged

The crossover is one to two vectors. Below `len=16` the two are
indistinguishable, because the window scan handles those without calling
the bulk scanner. At exactly `len=16` the vectorized build is 15-24% slower:
a run that ends precisely at the window boundary pays for a call that finds
its answer in the first byte. From `len=32` upward the vectorized scanner
wins. Non-ASCII text is only handed over when it looks dense (the byte after
the current sequence is also non-ASCII), so a lone accented letter in ASCII
text still takes the scalar decoder.

### Regressions this regime caught

The first stacked run of CL 2 showed encoding 35-70% *slower* on
`SyntheaFhir`, `TwitterStatus` and `GolangSource`, none of which reach a
vector very often, while decoding was fine. The scan was returning with the
upper halves of the YMM registers dirty, and every legacy-SSE instruction
after it, such as the small copies in `memmove` that the encoder issues
right after each scan, paid the AVX-to-SSE transition penalty.
`archsimd.ClearAVXUpperBits` (VZEROUPPER) before each return fixed it. An
earlier, un-stacked comparison had not shown it: the machine had evidently
been on a different host, and the penalty depends on the microarchitecture.

The first version of the UTF-8 validator kept its vector constants in a
struct. A struct with that many fields is not SSA-able, so every use went
through memory, and short non-ASCII strings measured up to 13x *slower*
than the scalar decoder. Moving the state into local variables made them
faster than scalar from 16 bytes up.

The first version of CL 7 called `bytes.IndexByte` for every object; on the
two- and three-name objects that make up CitmCatalog that call cost more
than the two comparisons it replaced (+9-12% encode), which is why small
objects now use a plain loop.

The first version of CL 8 replaced the number state machine and changed
nothing on CanadaGeometry: the digits were the work, not the machine. The
SWAR digit scan is what produced the -19%. A patch to the decoder call sites
had also silently failed to apply, and the tests could not tell, because the
scanner was correct and merely unused; only the unchanged profile did.

Measuring the chain per commit on arm64, where nothing was vectorized until
the last CL, showed that the AVX2 scanners had been hiding scalar-path
regressions on amd64. None of them was algorithmic; all were found by
bisecting to the commit, then putting the old and new function in one
binary over the real document and reading the assembly:

- The SWAR whitespace scan indexed `b[n]..b[n+7]` under `len(b)-n >= 8`,
  which the prover cannot use, so every word carried eight bounds checks —
  more instructions than the classification. Indexing through `p := b[n:]`
  leaves one. CitmCatalog was +16% on the scalar build from this alone.
- The single space after a colon, 37% of all whitespace runs in
  SyntheaFhir, went out of line through two calls, the first with a frame.
  `ConsumeWhitespace` has no inlining budget left (cost 78 of 80), so the
  scanner decides that case first and the generic wrapper inlines away.
- `jsontext.Value.Kind` inlined `ConsumeWhitespace`; once that was a call
  its cost was 99 and it stopped inlining at ten sites in `encoding/json/v2`,
  one per string value unmarshaled.
- The window-scan path in `ConsumeStringResumable` fell through into the
  rune path, and at that join the compiler reloaded and re-bounds-checked
  `b[n]`: four instructions per rune, +2.6% on StringUnicode Unmarshal.
  `continue` after the window gives the rune path one predecessor.
- `Token.string`, out of line already, called the no-longer-inlinable
  `ConsumeSimpleString` for every object name Unmarshal matched: +1-2% on
  CitmCatalog Unmarshal until it used the window scan like the decoder.
- The signature filter kept its signatures in a slice grown by `append`,
  +18 allocs/op on TwitterStatus; a fixed array in the namespace holds the
  64 the linear search can ever see.

And one that only the `GOEXPERIMENT=simd` build had: every Marshal of a
string longer than 32 bytes allocated. `archsimd.LoadUint8x32Array` was a
body-less function, and escape analysis assumes such a function writes
through its pointer parameters, so the scanned bytes were marked mutated
all the way up to `AppendQuote`'s `[]byte(s)` argument, and the zero-copy
string conversion there was disabled. The scanners' own benchmarks report
0 B/op, so only the end-to-end allocs/op column showed it. The load
intrinsics now carry the one-line body they implement (CL 1), which is
enough for escape analysis to see a read.

## When a benchmark disagrees with expectations

Three things explain most surprises in this package, in the order worth
checking:

1. **Inlining.** These scanners live inside loops whose callers care a great
   deal about inlining, and adding a single call to a function is enough to
   push it over the inliner's budget and slow down every short string that
   never needed the call. Check with:

       GOEXPERIMENT=simd go build -gcflags=-m=2 ./... 2>&1 | grep 'inline ConsumeShortSimpleString'

   `cannot inline ...: function too complex: cost N exceeds budget 80` is the
   message to watch for. This is why `ConsumeShortSimpleString` is split out
   from `ConsumeSimpleString` at all.

2. **Bounds checks.** Bounding a scan to a window can cost a second comparison
   per byte if the compiler can no longer prove the index is in range. The
   window scanners truncate the slice rather than tracking a separate limit
   precisely so that the loop keeps the shape the prover recognizes. Check with
   `go build -gcflags=-S` and look for `panicBounds` inside the loop body.

3. **Where the time actually is.** Profiles here are frequently more useful
   than the benchmark numbers, because a scan that got twice as fast moves the
   total very little if scanning was a fifth of the work:

       go test -run=XXX -bench=BenchmarkTestdata/TwitterStatus/Decode -benchtime=3s -cpuprofile=cpu.prof
       go tool pprof -top -nodecount=15 cpu.prof

4. **The allocs/op column.** A scanner that copies nothing can still cause a
   copy elsewhere: escape analysis is whole-program, and a change in what it
   can prove about a leaf shows up as an allocation in a caller several
   frames up. The scanner benchmarks cannot see that; `BenchmarkTestdata`
   can, so read its allocs/op alongside ns/op every time.

## Correctness

Speed measurements are only worth reading if the fast path computes the same
answer. `scan_test.go` checks the vectorized scanners against the scalar
references exhaustively for every special byte at every offset of every length
up to several vectors, at every alignment, on random JSON-shaped input, and
under fuzzing:

    GOEXPERIMENT=simd go test -run='TestScan|TestIndex' ./...
    GOEXPERIMENT=simd go test -run=FuzzIndexScanners -fuzz=FuzzIndexScanners -fuzztime=1m .

The nibble classification tables are checked against the predicates they encode
rather than transcribed by hand, so a table and its predicate cannot drift
apart silently.
