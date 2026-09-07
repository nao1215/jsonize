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
with the machine noted here. Inputs are synthetic but format-faithful
(`df`, `mount`, `env` at 10 and 100 000 rows), so results are
reproducible without external fixtures.

| Benchmark | Measures |
|-----------|----------|
| `LoadRegistry/definitions=N` | indexing a registry, paid on every jz start; N brackets the official registry (26) and one twenty times larger |
| `LoadAndValidate`, `ValidateNested` | decoding and validating one definition |
| `Detect/definitions=N` | `COMMAND \| jz`: every signature is evaluated because no parser was named |
| `DetectWithParser`, `DetectWithVariant` | `--parser` and `--variant`, which narrow the scan to one command or one definition |
| `DetectNoMatch` | the worst case, where every candidate is evaluated and rejected |
| `DetectLargeInput` | detection over a 1 MB input, to show the cost follows the signature window and not the input size |
| `ParseTable*`, `ParseRegex*`, `ParseKV*` | the engine on small and large inputs |
| `EncodeJSON*` | JSON generation (compact and pretty) |

Detection is a two-stage design: cheap signature matching over the first
lines picks exactly one definition, and only that definition then parses
the whole input. No definition is ever fully applied speculatively.
