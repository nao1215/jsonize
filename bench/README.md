# Benchmarks

`baseline.txt` is the reference `go test -bench` output committed with the
code. It was recorded with:

```
go test -run '^$' -bench . -benchmem -count 3 ./internal/... ./pkg/...
```

on an AMD Ryzen AI Max+ 395 (32 threads), Linux, Go 1.26. Absolute numbers
depend on the machine; what matters is the ratio between a new run and
the baseline on the same machine:

```
make bench            # writes bench/new.txt (COUNT=6 by default)
make bench-compare    # benchstat bench/baseline.txt bench/new.txt
```

Refresh the baseline only for an intentional change, in the same commit,
with the machine noted here. It is for this machine and no other: a
comparison against it on another one measures the two machines. CI does
not use it, and compares a pull request's base against its head, both run
on the runner that is comparing them. Inputs are synthetic but format-faithful
(`df`, `mount`, `env` at 10 and 100 000 rows), so results are
reproducible without external fixtures.

| Benchmark | Measures |
|-----------|----------|
| `LoadRegistry/definitions=N` | indexing a synthetic registry of N definitions, the cost every jz start pays; N is 26, 500 and 1000, so the curve shows how the cost grows with the size of a registry, and the official one, a few hundred definitions (`jz list` names them), falls inside it |
| `LoadEmbedded` | loading the official registry as it is built into jz, which is what `df -h \| jz` pays before reading any text |
| `LoadAndValidate`, `ValidateNested` | decoding and validating one definition |
| `Detect/definitions=N` | `COMMAND \| jz` over the same synthetic sizes: every signature is evaluated because no parser was named |
| `DetectWithParser`, `DetectWithVariant` | `--parser` and `--variant`, which narrow the scan to one command or one definition |
| `DetectNoMatch` | the worst case, where every candidate is evaluated and rejected |
| `DetectLargeInput` | detection over a 1 MB input, to show the cost follows the signature window and not the input size |
| `ParseTable*`, `ParseRegex*`, `ParseKV*` | the engine on small and large inputs |
| `EncodeJSON*` | JSON generation (compact and pretty) |
| `StreamTableLarge` | `--stream` over the input `ParseTableWhitespaceLarge` reads whole, so the two are a pair |
| `ReadCSV`, `Read/*` | the data formats `--format` and a file's extension name |
| `PlanBuild*`, `FixedEach` | `jz new`: the arguments read into a plan and built, and the part `--each` does per record |
| `KeyFilter` | `--extract` and `--exclude`, against no key option at all |
| `Convert` | one whole run of jz on a short df report, which is what a pipeline pays for each `\| jz` |

Detection is a two-stage design: cheap signature matching over the first
lines picks exactly one definition, and only that definition then parses
the whole input. No definition is ever fully applied speculatively.
