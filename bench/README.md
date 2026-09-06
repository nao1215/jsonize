# Benchmarks

`baseline.txt` is the reference `go test -bench` output committed with the
code. It was recorded with:

```
go test -run '^$' -bench . -benchmem -count 3 ./internal/...
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
| `LoadRegistry/definitions=N` | indexing a registry, paid on every jz start |
| `LoadAndValidate`, `ValidateNested` | decoding and validating one definition |
| `Select` | variant selection among 8 candidates |
| `ParseTable*`, `ParseRegex*`, `ParseKV*` | the engine on small and large inputs, including raw mode |
| `EncodeJSON*` | JSON generation (compact and pretty) |
