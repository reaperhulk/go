#!/usr/bin/env python3
"""Run identical binaries in alternating order; preserve all raw measurements."""

import argparse
import csv
import os
from pathlib import Path
import re
import subprocess

p = argparse.ArgumentParser(description=__doc__)
p.add_argument("--binary", action="append", required=True, help="label=/absolute/path")
p.add_argument("--out", type=Path, required=True)
p.add_argument("--valgrind", help="Callgrind executable; omit for native Go benchmarks")
p.add_argument("--repeat", type=int, default=3)
p.add_argument("--benchtime", default="200ms")
p.add_argument("--scale", type=int, default=1, help="multiply fixed iteration counts")
p.add_argument("--cases", default="Small,ASCII8,ASCII32,ASCII256,ASCII4096,EscapeRuns,UnicodeRuns,GolangSource,StringEscaped,StringUnicode,TwitterStatus,CanadaGeometry")
p.add_argument("--ops", default="Marshal,Unmarshal")
args = p.parse_args()
binaries = [b.split("=", 1) for b in args.binary]
args.out.mkdir(parents=True, exist_ok=True)
env = dict(os.environ, GOMAXPROCS="1")
if args.valgrind:
    # Isolate operation cost from GC timing and asynchronous signal delivery.
    # Native measurements retain the normal garbage collector.
    env.update(GOGC="off", GODEBUG="asyncpreemptoff=1")
rows = []
for case in args.cases.split(","):
    n = 200 if case in {"Small", "EscapeRuns", "UnicodeRuns"} or case.startswith("ASCII") else 3
    n *= args.scale
    for op in args.ops.split(","):
        for repeat in range(args.repeat):
            order = binaries if repeat % 2 == 0 else binaries[::-1]
            for label, binary in order:
                stem = args.out / f"{case}-{op}-{repeat}-{label}"
                cmd = [binary, "-case", case, "-op", op]
                if args.valgrind:
                    cmd = [args.valgrind, "--tool=callgrind", "--instr-atstart=no",
                           "--cache-sim=no",
                           "--branch-sim=yes", f"--callgrind-out-file={stem}.callgrind"] + cmd + ["-n", str(n)]
                else:
                    cmd += ["-native", f"-test.benchtime={args.benchtime}"]
                result = subprocess.run(cmd, env=env, capture_output=True, text=True)
                stem.with_suffix(".stdout").write_text(result.stdout)
                stem.with_suffix(".stderr").write_text(result.stderr)
                result.check_returncode()
                if args.valgrind:
                    raw = stem.with_suffix(".callgrind").read_text()
                    events = re.search(r"^events: (.*)$", raw, re.M)[1].split()
                    totals = list(map(int, re.search(r"^totals: (.*)$", raw, re.M)[1].split()))
                    counters = dict(zip(events, totals))
                    if counters.get("Ir", 0) == 0:
                        raise RuntimeError("main.measure did not collect instructions")
                    rows.append(dict(case=case, op=op, repeat=repeat, binary=label, n=n,
                                     **{k: v / n for k, v in counters.items()}))
                    print(case, op, label, counters["Ir"] / n, "Ir/op", flush=True)
                else:
                    with (args.out / f"{label}.txt").open("a") as f:
                        f.write(result.stdout)
                    print(result.stdout.strip(), label, flush=True)
if rows:
    with (args.out / "counts.csv").open("w") as f:
        writer = csv.DictWriter(f, fieldnames=rows[0].keys())
        writer.writeheader()
        writer.writerows(rows)
