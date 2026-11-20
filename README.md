<p align="center">
  <a href="https://nao1215.github.io/jsonize/">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="doc/images/jsonize-logo-dark.png">
      <img src="doc/images/jsonize-logo.png" alt="jsonize" width="420">
    </picture>
  </a>
</p>

[![CI](https://github.com/nao1215/jsonize/actions/workflows/unit_test.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/unit_test.yml)
[![E2E](https://github.com/nao1215/jsonize/actions/workflows/e2e.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/e2e.yml)
![Coverage](https://raw.githubusercontent.com/nao1215/octocovs-central-repo/main/badges/nao1215/jsonize/coverage.svg)
[![Gitleaks](https://github.com/nao1215/jsonize/actions/workflows/gitleaks.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/gitleaks.yml)
[![Trivy](https://github.com/nao1215/jsonize/actions/workflows/trivy.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/trivy.yml)
[![govulncheck](https://github.com/nao1215/jsonize/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/govulncheck.yml)
[![Gosec](https://github.com/nao1215/jsonize/actions/workflows/gosec.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/gosec.yml)
[![tested with atago](https://img.shields.io/badge/tested%20with-atago-7c3aed?logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI%2BPHBhdGggZmlsbD0iI2ZmZiIgZD0iTTMuNiA0LjIgMTEuOSAxMmwtOC4zIDcuOC0xLjktMi4yTDcuOSAxMiAxLjcgNi40eiIvPjxyZWN0IGZpbGw9IiNmZmYiIHg9IjEyLjYiIHk9IjE3LjIiIHdpZHRoPSI5LjciIGhlaWdodD0iMi44IiByeD0iMS40Ii8%2BPC9zdmc%2B&logoColor=white)](https://github.com/nao1215/atago)
[![measured with himorime](https://img.shields.io/badge/measured%20with-himorime-d9480f?logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI%2BPHBhdGggZmlsbD0ibm9uZSIgc3Ryb2tlPSIjZmZmIiBzdHJva2Utd2lkdGg9IjIuNCIgc3Ryb2tlLWxpbmVjYXA9InJvdW5kIiBkPSJNNC4yIDE4LjVBOSA5IDAgMSAxIDE5LjggMTguNSIvPjxwYXRoIGZpbGw9Im5vbmUiIHN0cm9rZT0iI2ZmZiIgc3Ryb2tlLXdpZHRoPSIyLjQiIHN0cm9rZS1saW5lY2FwPSJyb3VuZCIgZD0iTTEyIDE0LjUgMTYuNSA5Ii8%2BPGNpcmNsZSBmaWxsPSIjZmZmIiBjeD0iMTIiIGN5PSIxNC41IiByPSIyLjIiLz48L3N2Zz4=&logoColor=white)](https://github.com/nao1215/himorime)
[![Go Reference](https://pkg.go.dev/badge/github.com/nao1215/jsonize.svg)](https://pkg.go.dev/github.com/nao1215/jsonize)
[![GitHub Downloads (all assets, all releases)](https://img.shields.io/github/downloads/nao1215/jsonize/total)](https://github.com/nao1215/jsonize/releases)
[![License](https://img.shields.io/github/license/nao1215/jsonize)](./LICENSE)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/nao1215/jsonize/badge)](https://scorecard.dev/viewer/?uri=github.com/nao1215/jsonize)


# jsonize

jsonize makes JSON for the next command in a pipeline. `jz` turns what a
command printed, a data or text file, or the arguments it is given into
JSON, and stops there: choosing records and reshaping them stays with
`jq`.

```console
$ df -h | jz
[{"filesystem":"/dev/nvme0n1p2","size":"1.8T","used":"1.6T","available":"145G","use_percent":92,"mounted_on":"/"}, ...]

$ jz --file sales.csv --type units=int
[{"sku":"007","units":12},{"sku":"008","units":null}]

$ jz new --string "message=$MESSAGE" --path /metadata/labels/app=api replicas:=3
{"message":"@here: deploy := done","metadata":{"labels":{"app":"api"}},"replicas":3}
```

![jz reading df, uptime and free, and refusing input it cannot identify](demo/jsonize.gif)

jz identifies the output of more than two hundred commands from its
text. Each format is a YAML definition, so adding one needs no Go and no
rebuild.

Documentation: https://nao1215.github.io/jsonize/ ([Cookbook](https://nao1215.github.io/jsonize/cookbook/))

## Install

Go 1.26 or later:

```console
$ go install github.com/nao1215/jsonize/cmd/jz@latest
```

On Arch Linux, the [`jsonize-bin`](https://aur.archlinux.org/packages/jsonize-bin)
package in the AUR:

```console
$ yay -S jsonize-bin
```

Homebrew, on macOS and Linux, for releases after v0.1.0:

```console
$ brew install --cask nao1215/tap/jsonize
```

[GitHub Releases](https://github.com/nao1215/jsonize/releases) has
archives for Linux, macOS and Windows (amd64 and arm64), and `.deb`,
`.rpm` and `.apk` packages for Linux. See the
[install guide](https://nao1215.github.io/jsonize/install/) for archive
installation, verifying a download and bash/zsh completion.

CI tests Linux, macOS, Windows, and FreeBSD (the end-to-end suite in a virtual machine; install with `go install`). OpenBSD and NetBSD are built and linted, not run.

## Using it

```console
COMMAND | jz                   # command output, its format detected from the text
jz --file captured.txt         # the same, read from a file
jz --file users.csv            # a data file, read as the format its extension names
COMMAND | jz --format lines    # piped data or text, read as the format --format names
jz new KEY=VALUE KEY:=JSON     # JSON from arguments, files and standard input
jz run COMMAND [args...]       # run a command and convert what it prints
jz list                        # the command output jz reads
jz test [DIR...]               # check parser definitions of your own
```

```text
Input:
  -f, --file PATH               read input from PATH instead of stdin
      --format NAME             read the input as this format: csv, tsv, ltsv, jsonl, json, yaml, text, lines, nul
      --columns NAME,...        name the columns of a csv read without a header line
      --type COLUMN=TYPE        convert a csv or tsv column to int, float or bool (repeatable)

Output:
  -p, --pretty                  indent JSON output
      --stream                  write each record as a line of JSON as soon as it is read
      --extract KEY             keep only this key of each object (repeatable)
      --exclude KEY             drop this key from each object (repeatable)

Choosing and checking the parser of command output:
      --parser NAME             restrict detection to one parser
      --variant NAME            use a variant of --parser
      --define YAML             read with a definition given here instead of a registered one
      --explain[=json]          report the chosen definition and why, on stderr
      --raw                     show the text a definition cut, without its field rules (types, trims, null words)
      --assume-year YEAR        date the timestamps a format prints without a year (or "now")
      --assume-zone ABBR=+HHMM  give a zone abbreviation an offset (repeatable)

Help:
  -h, --help                    show help
```

### Command output

`--extract` and `--exclude` keep or drop keys; a key the output cannot
have is an error that lists the keys it has.

```console
$ df -h | jz --extract filesystem --extract mounted_on
[{"filesystem":"/dev/nvme0n1p2","mounted_on":"/"}, ...]
```

`jz run` passes standard input and standard error through, mirrors the
command's exit status, and runs it with `LC_ALL=C`.

### Files and text

A data file is read as the format its extension names (`.csv`, `.tsv`,
`.ltsv`, `.jsonl` or `.ndjson`, `.json`, `.yaml` or `.yml`, each also as
`.gz` or `.bz2`), and piped data as the format `--format` names.
`--format text`, `lines` and `nul` read plain text as one string or a
list of strings.

```console
$ git branch --format='%(refname:short)' | jz --format lines
["main","feature/login"]

$ kubectl get deploy api -o yaml | jz --format yaml --extract spec
{"spec":{"replicas":3, ...}}
```

### JSON from arguments

In `jz new`, `=` makes a string, `:=` reads JSON, `@path` reads a file
and `[]` appends to an array. `--path` places a value at a JSON Pointer.

```console
$ jz new name=api replicas:=3 'tags[]=web' spec:=@deploy.yaml
{"name":"api","replicas":3,"tags":["web"],"spec":{"replicas":3}}
```

### Streams

`--stream` writes one JSON document per record as it is read, and
`jz new --each` wraps each one.

```console
$ vmstat 1 | jz --stream | jz new --each --string host=web sample:=@-
{"host":"web","sample":{"r":1,"b":0,"swpd":0,"free":5123456,...}}
```

The [usage guide](https://nao1215.github.io/jsonize/usage/#reading-a-command-that-keeps-printing)
has the details.

## Detection and limits

A format too generic to identify needs a parser name:

```console
$ git diff --numstat | jz
jz: unable to identify the input format
it could be `git` output, but that format is too generic for jz to claim on its own

Confirm it:
  COMMAND | jz --parser git
```

A rounded size such as `"1.8T"` stays a string. Use `df`, `free` or
`lsblk -b` for exact numbers. The `size` field from `ls -l` also stays a
string because the same format reads `ls -lh`.

Naming a parser does not skip the checks: the definition's signature
still has to fit, and no option turns that off.

The [coverage page](https://nao1215.github.io/jsonize/coverage/) lists which forms of which commands are read and which are refused.

Of the commands Windows prints, jz reads `ipconfig`, `ipconfig /all`
and `systeminfo`. CI runs them on English Windows Server 2022 and 2025;
other display languages and code pages are not checked.

## Compared with jc and jo

[jc](https://github.com/kellyjonbrazil/jc) converts command output, file formats and strings, and [jo](https://github.com/jpmens/jo) builds JSON from arguments. jz overlaps with part of each. Both are older and packaged by the major distributions, and each is the better choice for some jobs.

jc uses a parser you name or one taken from the command name, also reads strings such as URLs and timestamps, and is extended with Python plugins. jz detects the format from the text, refuses input that does not fit with exit status 3 or 4, and is extended with YAML definitions. jo guesses value types; `jz new` does not.

The [comparison page](https://nao1215.github.io/jsonize/compare/) has the feature table for jc 1.25.7, jo 1.9 and jz v0.10.0, and timings measured with [himorime](https://github.com/nao1215/himorime): jz is faster with the parser named and on large input, while jc is faster when jz has to detect the format of a short output on one core, and jo builds objects faster than `jz new`.

## What jsonize does not do

jz does not query or reshape JSON (use `jq`), and a command's native JSON output is the better choice when it has one. Input must match a known or explicitly defined format: ambiguous input needs `--parser`, and text jz cannot read is an error, not dropped. Lines a definition names as not data, such as headings and `total` lines, are left out; `--explain` counts them. Conversion never touches the network, and `jz run` does not start a shell.

## Adding a parser

Add a YAML definition, a captured `.txt` fixture and its `.yaml` capture
metadata, then run `jz test --update` to generate its expected JSON.
It rewrites the expected JSON of every fixture it reads, so review each
changed file, not only the new one. Then run `jz test` to check the
definition against its fixtures and the official fixture corpus.

The [guide](https://nao1215.github.io/jsonize/write-a-parser/) and the
[definition reference](https://nao1215.github.io/jsonize/definition-format/)
cover registry layering and the YAML.

## From Go

Use [registry](https://pkg.go.dev/github.com/nao1215/jsonize/pkg/registry)
to load definitions,
[selector](https://pkg.go.dev/github.com/nao1215/jsonize/pkg/selector)
to identify the input, and
[engine](https://pkg.go.dev/github.com/nao1215/jsonize/pkg/engine)
to parse it. The embedded registry is available from
[`registry.FS`](https://pkg.go.dev/github.com/nao1215/jsonize/registry#FS).
The four in one runnable piece are in
[the selector example](https://pkg.go.dev/github.com/nao1215/jsonize/pkg/selector#example-package).

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | unexpected failure |
| 2 | usage error |
| 3 | the input did not match the chosen definition, or held text the definition did not read |
| 4 | unidentified, ambiguous, or a named parser that did not fit |
| 5 | a registry could not be loaded |
| 141 | standard output was closed early, as by a pipe into `head`; nothing is said, and a command `jz run` started is stopped |
| *n* | `jz run` mirrors the command's own status, or 128+signal |

Diagnostics go to standard error. A failure leaves standard output
empty, except with `--stream` and `jz new --each`, where the records
already written remain.

## Testing

| Technique | Bug class caught here |
|---|---|
| [Golden fixtures of real output](registry/registry_test.go) | A parser that reads a captured command output differently from the JSON recorded next to it |
| [Conformance relations](internal/conformance) | Output that changes with a BOM, CRLF, a missing final newline or the same output twice in a row |
| [Decoys](registry/testdata/decoys) | Input of one command identified as another |
| [Generated properties](pkg/engine/engine_test.go) | Tables that lose, move or retype a cell, numbers that do not survive their digit grouping, and stream and whole readings that disagree |
| [Differential against encoding/json](pkg/jsonutil/object_test.go) | JSON written differently from what encoding/json writes for the same value |
| [Independent schema validator](scripts/check_schemas.py) | A fixture that does not fit its published schema, checked by Python jsonschema rather than jz's own validator |
| [Fuzzing](.github/workflows/fuzz.yml) | Panics, hangs and unbounded memory on malformed definitions and input |
| [Mutation testing](.github/workflows/mutation.yml) | Logic a change can break without a test failing |
| [atago end-to-end scenarios](e2e/atago) | The binary on Linux, macOS, Windows and FreeBSD reading the real commands of the host |
| [himorime benchmarks](bench) | A pull request that makes jz slower or larger in memory than its base |

The commands, the recorded mutation scores and where each runs in CI are on the [testing page](https://nao1215.github.io/jsonize/testing/).

## Development

```console
$ make help                    # every target
$ make check                   # fmt, vet, lint, unit tests, race
$ make e2e                     # atago end-to-end suite (see e2e/README.md)
$ make registry-test           # the same checks `jz test` runs, on the official registry
$ make website-serve           # the documentation site locally
$ make demo                    # re-record demo/jsonize.gif with vhs
```

## Contributors

- [Naohiro CHIKAMATSU](https://github.com/nao1215): code and documentation
- [Rafael Baboni Dominiquini](https://github.com/Dominiquini): the
  [`jsonize-bin`](https://aur.archlinux.org/packages/jsonize-bin) AUR package

## License

[MIT](LICENSE)
