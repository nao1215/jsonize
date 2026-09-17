# Benchmarks

`himorime.yaml` measures jz the way a pipeline runs it: one process per
call, from start to exit. [himorime](https://github.com/nao1215/himorime)
builds jz, runs each command in interleaved rounds, and reports latency,
CPU time, peak RSS and, for the large inputs, throughput.

```console
$ go install github.com/nao1215/himorime@latest
$ make bench            # himorime run bench
$ make bench-compare    # himorime compare --against main bench
```

`make bench-compare` checks main out into a temporary Git worktree, builds
it and your working tree, and measures both in the same rounds, so a
background job slows both revisions instead of one. `BASE=v0.3.0 make
bench-compare` compares against another revision.

On a pull request, `.github/workflows/bench.yml` runs `himorime ci bench`
the same way, with the base of the pull request as the base, on one
runner. The job fails when a command is slower, uses more CPU time or more
memory than the base beyond the tolerance in `himorime.yaml` with 95%
confidence. A difference too close to call is reported as inconclusive and
does not fail the job. The comparison is on the job summary page.

The inputs are generated before measuring, so no fixture is committed:

| Benchmark | Commands | Measures |
|-----------|----------|----------|
| `version` | `jz version` | starting jz, with no registry loaded |
| `detect df` | `jz`, `jz --parser df` on a short `df -h` report | detecting the format among every built-in definition, against reading only the definitions of df, which is also what `jz run df` does |
| `ps table 100k rows` | `jz --parser ps`, the same with `--stream` | a large table read whole and read record by record; peak RSS shows what `--stream` does not hold |
| `csv 100k rows` | `jz --file users.csv --type age=int`, the same with `--extract` | a data file with a typed column, and key selection |
| `jsonl 100k records` | `jz --format jsonl`, the same with `--stream` | JSON Lines read as one document and passed through |
| `jz new` | `jz new` with arguments, `jz new --each` over 100 000 records | building an object from arguments, and attaching fixed values to each record of a stream |

Numbers from different machines are not comparable; compare revisions on
one machine, as `make bench-compare` and CI do.

The `Benchmark*` functions in the `bench_test.go` files measure single
functions (registry loading, detection, the engine, JSON encoding) for
profiling with `go test -bench`. They are not part of the comparison.

`scripts/compare_bench.sh` is a different measurement: jz against jc and jo
on the same input, for the comparison page of the documentation.
