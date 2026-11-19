---
title: Testing
description: "How jsonize is tested: the techniques, the bugs each one is there to catch, the commands that run them and where CI runs them."
toc: true
---

Each technique below is here because it catches a kind of bug the others miss. The commands run from the root of a checkout.

## Golden fixtures of real output

Every definition in `registry/parsers/` has captured output of the real command next to it, with the JSON it must become. `make registry-test` reads each fixture through detection and through the named parser and compares the JSON. `make registry-update-golden` rewrites the expected JSON, and `git diff` then shows every change a definition made. CI: `unit_test.yml`, every pull request.

## Conformance relations

`internal/conformance` checks relations every fixture must keep, without an expected file of their own: the same JSON with a byte order mark, with CRLF line ends, without the final newline and with blank lines after it, and the records of an array definition twice when its output is given twice. The same checks run as `jz test` on a registry of one's own. CI: `unit_test.yml`.

## Decoys

`registry/testdata/decoys/` holds text that resembles a command's output but is not it, and detection must refuse each. CI: `unit_test.yml`.

## Generated properties

`internal/proptest` runs a property on generated inputs with the standard library alone, from a seed it prints when the property fails (`JSONIZE_PROPERTY_SEED` replays it, `JSONIZE_PROPERTY_RUNS` sets the number of inputs). `go test` checks each property on 200 inputs; the nightly `properties` job of `fuzz.yml` checks a million.

| Property | Where |
|---|---|
| A whitespace or aligned table of any shape reads back as the cells it was written from, with wide characters counted as two columns, and streaming it gives the records reading it whole gives | `pkg/engine/engine_test.go`, `TestTableReadsBackTheCellsItWasWritten` |
| Any integer grouped in threes with any separator comes back through `Ungroup` as the number, and a group of two digits is refused | `pkg/convert/convert_test.go`, `TestUngroupRoundTrip` |
| The JSON jz writes, compact and indented, is byte for byte what encoding/json writes for the same value with HTML escaping off | `pkg/jsonutil/object_test.go`, `TestEncodeMatchesEncodingJSON` |

## Independent schema validator

jz validates its output against the schemas it publishes with its own validator. `scripts/check_schemas.py` checks every fixture against its schema with the Python `jsonschema` package as well, so the two agreeing is evidence rather than one implementation agreeing with itself. CI: `schema.yml`, which also refuses a schema change that breaks the published contract.

## Fuzzing

Fifteen fuzz targets cover the definition loader, the engine, detection, the conversions, the data readers, the YAML reader, `jz new` and the schema generator. Run one with `go test -run '^$' -fuzz '^FuzzParse$' -fuzztime 1m ./pkg/engine`. CI: `fuzz.yml` runs each for five minutes every night and fails when a target is missing from its matrix.

## Mutation testing

[gremlins](https://github.com/go-gremlins/gremlins) v0.6.0 changes one expression at a time and checks that a test fails. A pull request mutates the lines it changes; every Sunday each package that computes is mutated whole (`mutation.yml`). Run one package with `gremlins unleash --workers 2 ./pkg/engine` from the root.

The score is killed / (killed + lived). Mutants no test reaches are counted apart as not covered. The values below are the first full run, and a later run is compared with them rather than with a fixed threshold, since some surviving mutants change nothing observable.

<!-- mutation-scores:start -->
First full run on 2026-11-19 (gremlins v0.6.0, before the generated properties were added):

| Package | Killed | Lived | Not covered | Score |
|---|---|---|---|---|
| `pkg/convert` | 194 | 35 | 25 | 84.72% |
| `pkg/definition` | 366 | 57 | 53 | 86.52% |
| `pkg/engine` | 484 | 79 | 38 | 85.97% |
| `pkg/jsonutil` | 63 | 3 | 7 | 95.45% |
| `pkg/registry` | 32 | 14 | 7 | 69.57% |
| `pkg/selector` | 171 | 38 | 17 | 81.82% |
| `internal/datafile` | 105 | 21 | 48 | 83.33% |
| `internal/jsonbuild` | 134 | 32 | 4 | 80.72% |
| `internal/schema` | 144 | 27 | 38 | 84.21% |
| `internal/yaml` | 293 | 31 | 77 | 90.43% |
| all | 1986 | 337 | 314 | 85.49% |

With the generated properties and the boundary tests of `ReadBounded` and the fixtures directory added the same day: `pkg/convert` 85.15%, `pkg/engine` 86.15%, `pkg/jsonutil` 97.01% and `pkg/registry` 84.09%.
<!-- mutation-scores:end -->

## atago end-to-end scenarios

`e2e/atago/` runs the built binary on Linux, macOS, Windows and FreeBSD, on fixtures and on the real commands of the machine, with [atago](https://github.com/nao1215/atago). `make e2e` runs them locally. CI: `e2e.yml`.

## himorime benchmarks

`bench/` measures jz end to end with [himorime](https://github.com/nao1215/himorime). On a pull request `bench.yml` builds the base and the head and measures them in alternating rounds on one runner, and fails when the head is slower or uses more memory beyond the tolerance of the suite. `make bench-compare` does the same locally against main.
