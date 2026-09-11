#!/usr/bin/env bash
# Copyright 2026 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

# benchcmp.sh compares a JSON benchmark between a build with the vectorized
# scanners (GOEXPERIMENT=simd) and one without.
#
# It builds each variant once, runs them interleaved so that drift in machine
# speed hits both equally rather than whichever one ran last, and hands the
# results to benchstat. On a shared machine interleaving matters more than run
# count: a hundred runs of A followed by a hundred of B will report a confident
# difference that is really just the machine warming up.
#
# Usage:
#	benchcmp.sh [-p pkg] [-b regexp] [-n runs] [-t benchtime] [-o dir]
#
#	-p  package to benchmark (default encoding/json/v2)
#	-b  -bench regexp (default BenchmarkTestdata/.*/Value/Buffered)
#	-n  number of interleaved runs of each variant (default 8)
#	-t  -benchtime value (default 100ms)
#	-o  directory to keep the raw benchmark output in (default a temp dir)
#
# To compare against another commit instead, build that commit's test binary
# first and pass its output to benchstat alongside these:
#
#	git stash && go test -c -o /tmp/base.test encoding/json/v2 && git stash pop
#
# It needs benchstat: go install golang.org/x/perf/cmd/benchstat@latest

set -euo pipefail

pkg=encoding/json/v2
bench='BenchmarkTestdata/.*/(Encode|Decode)/Value/Buffered'
runs=8
benchtime=100ms
out=

while getopts "p:b:n:t:o:" opt; do
	case "$opt" in
	p) pkg=$OPTARG ;;
	b) bench=$OPTARG ;;
	n) runs=$OPTARG ;;
	t) benchtime=$OPTARG ;;
	o) out=$OPTARG ;;
	*) exit 2 ;;
	esac
done

if [ -z "$out" ]; then
	out=$(mktemp -d)
	trap 'rm -rf "$out"' EXIT
fi
mkdir -p "$out"

echo "building scalar variant" >&2
GOEXPERIMENT=nosimd go test -c -o "$out/scalar.test" "$pkg"
echo "building simd variant" >&2
GOEXPERIMENT=simd go test -c -o "$out/simd.test" "$pkg"

: > "$out/scalar.txt"
: > "$out/simd.txt"

for i in $(seq 1 "$runs"); do
	echo "run $i/$runs" >&2
	for v in scalar simd; do
		"$out/$v.test" -test.run=XXX -test.bench="$bench" \
			-test.benchtime="$benchtime" -test.count=1 >> "$out/$v.txt"
	done
done

benchstat -col .file scalar="$out/scalar.txt" simd="$out/simd.txt"
