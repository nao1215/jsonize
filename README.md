![Coverage](https://raw.githubusercontent.com/nao1215/octocovs-central-repo/main/badges/nao1215/jsonize/coverage.svg)
[![UnitTest](https://github.com/nao1215/jsonize/actions/workflows/unit_test.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/unit_test.yml)
[![Lint](https://github.com/nao1215/jsonize/actions/workflows/lint.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/lint.yml)
[![E2E](https://github.com/nao1215/jsonize/actions/workflows/e2e.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/e2e.yml)
[![tested with atago](https://img.shields.io/badge/tested%20with-atago-7c3aed?logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI%2BPHBhdGggZmlsbD0iI2ZmZiIgZD0iTTMuNiA0LjIgMTEuOSAxMmwtOC4zIDcuOC0xLjktMi4yTDcuOSAxMiAxLjcgNi40eiIvPjxyZWN0IGZpbGw9IiNmZmYiIHg9IjEyLjYiIHk9IjE3LjIiIHdpZHRoPSI5LjciIGhlaWdodD0iMi44IiByeD0iMS40Ii8%2BPC9zdmc%2B&logoColor=white)](https://github.com/nao1215/atago)
[![Go Reference](https://pkg.go.dev/badge/github.com/nao1215/jsonize.svg)](https://pkg.go.dev/github.com/nao1215/jsonize)
![GitHub](https://img.shields.io/github/license/nao1215/jsonize)
[![GitHub Downloads (all assets, all releases)](https://img.shields.io/github/downloads/nao1215/jsonize/total)](https://github.com/nao1215/jsonize/releases)

# jsonize

jsonize turns command output into JSON. Pipe a command to `jz` and it
works out which command produced the text and how to read it, so there is
nothing to name and nothing to configure.

```console
$ df -h | jz
[{"filesystem":"/dev/nvme0n1p2","size":"1.8T","used":"1.6T","available":"145G","use_percent":92,"mounted_on":"/"}, ...]

$ ps aux | jz | jq '.[] | select(.cpu_percent > 10) | .command'
"/usr/lib/firefox/firefox"
```

![jz reading df, uptime and free, and refusing input it cannot identify](demo/jsonize.gif)

Parsers are YAML definitions in a registry, not Go code, so another
command or another system's variant of one is a file and a fixture rather
than a release.

Documentation: https://nao1215.github.io/jsonize/

## Install

```console
$ go install github.com/nao1215/jsonize/cmd/jz@latest
```

Release archives for Linux, macOS and Windows are attached to every
release.

## Using it

```console
COMMAND | jz                 # convert piped output
jz < captured.txt            # or output captured earlier
jz --file captured.txt
jz run COMMAND [args...]     # let jz run the command and convert its stdout
jz list                      # what jz can read
jz test [DIR...]             # check parser definitions of your own
```

Options:

```text
  -f, --file PATH               read input from PATH instead of stdin
  -p, --pretty                  indent JSON output
      --stream                  write one record per line as it is read
      --raw                     skip the field rules and report every value as text
      --extract KEY             keep only this key (repeatable)
      --exclude KEY             drop this key (repeatable)
      --assume-year YEAR        date the timestamps a format prints without a year (or "now")
      --assume-zone ABBR=+HHMM  give a zone abbreviation an offset (repeatable)
      --parser NAME             restrict detection to one parser
      --variant NAME            use a variant of --parser
      --define YAML             read with a definition given here instead of a registered one
      --explain                 report the chosen definition and why, on stderr
  -h, --help                    show help
```

`--extract` and `--exclude` name keys of the objects jz prints, and
either may be repeated. Naming a key the format does not produce is an
error listing the keys it does have, because a document quietly missing
what was asked for is the wrong answer this tool exists to avoid.

```console
$ df -h | jz --extract filesystem --extract mounted_on
[{"filesystem":"/dev/nvme0n1p2","mounted_on":"/"}, ...]
```

`jz run` hands the command your standard input, passes its standard error
through, mirrors its exit status and runs it with `LC_ALL=C`. Everything
after the command name belongs to the command, and a bare `--` states
that boundary explicitly. An interrupt reaches the command, and a
command that keeps printing after its reader has gone, or past
`--timeout`, is stopped rather than left running.

`--stream` answers a command that keeps printing, one JSON document per
line as each record is read:

```console
$ jz run --stream ping -c 100 1.1.1.1 | jq -c 'select(.time_ms > 20)'
```

## What it will not do

Wrong JSON returned with a zero exit status is the failure that matters,
so jz refuses rather than approximates:

```console
$ git diff --numstat | jz
jz: unable to identify the input format
it could be `du` output, but that format is too generic for jz to claim on its own

Confirm it:
  COMMAND | jz --parser du
```

A rounded size stays the text that was printed. `df -h` counts 1024 per
suffix step and `df -H` counts 1000, the output records neither, and both
round, so a byte count would be two guesses stacked. Run `df`, `free`,
`ls -l` or `lsblk -b` when you need numbers.

Naming a parser is a claim about the input, not a way past the checks:
the definition's signature still has to fit, and there is no option that
turns that off.

## Adding a parser

A definition and a captured fixture, no Go:

```console
$ mkdir -p ~/.config/jsonize/registry/parsers/greet/default/testdata
$ $EDITOR ~/.config/jsonize/registry/parsers/greet/default/parser.yaml
$ jz test --update            # write testdata/<case>.json from the output
$ jz test                     # check it, and check it reads nothing else
$ greet | jz
```

The official fixtures are built into the binary, so `jz test` also proves
your definition refuses every format jz already reads.

Registries are layered: `JSONIZE_REGISTRY_PATH`, then the user registry
under the config directory, then the one built into the binary. jz never
accesses the network, so the same input converts to the same JSON on the
same machine. The guide is at
https://nao1215.github.io/jsonize/write-a-parser/ and the reference at
https://nao1215.github.io/jsonize/definition-format/.

## From Go

The registry, the selector and the engine are a library:

```go
reg, _ := registry.Load(registry.Source{Name: "embedded", FS: official.FS()})
sel, err := selector.Select(reg, selector.Context{Input: text})  // err: unidentified or ambiguous
out, err := engine.Parse(sel.Entry.Def, text, engine.Options{})  // out: ordered objects
jsonutil.Encode(os.Stdout, out, true)
```

Reference: https://pkg.go.dev/github.com/nao1215/jsonize/pkg/selector

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | unexpected failure |
| 2 | usage error |
| 3 | the input did not match the chosen definition |
| 4 | unidentified, ambiguous, or a named parser that did not fit |
| 5 | a registry could not be loaded |
| 141 | standard output was closed early, as by a pipe into `head`; nothing is said, and a command `jz run` started is stopped |
| *n* | `jz run` mirrors the command's own status, or 128+signal |

Diagnostics go to standard error. Standard output carries a complete JSON
document or nothing.

## Development

```console
$ make help                    # every target
$ make check                   # fmt, vet, lint, unit tests, race
$ make e2e                     # atago end-to-end suite (see e2e/README.md)
$ make registry-test           # the same checks `jz test` runs, on the official registry
$ make website-serve           # the documentation site locally
$ make demo                    # re-record demo/jsonize.gif with vhs
```

## License

[MIT](LICENSE)
