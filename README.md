<p align="center">
  <a href="https://nao1215.github.io/jsonize/">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="doc/images/jsonize-logo-dark.png">
      <img src="doc/images/jsonize-logo.png" alt="jsonize" width="420">
    </picture>
  </a>
</p>

![Coverage](https://raw.githubusercontent.com/nao1215/octocovs-central-repo/main/badges/nao1215/jsonize/coverage.svg)
[![UnitTest](https://github.com/nao1215/jsonize/actions/workflows/unit_test.yml/badge.svg)](https://github.com/nao1215/jsonize/actions/workflows/unit_test.yml)
[![tested with atago](https://img.shields.io/badge/tested%20with-atago-7c3aed?logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI%2BPHBhdGggZmlsbD0iI2ZmZiIgZD0iTTMuNiA0LjIgMTEuOSAxMmwtOC4zIDcuOC0xLjktMi4yTDcuOSAxMiAxLjcgNi40eiIvPjxyZWN0IGZpbGw9IiNmZmYiIHg9IjEyLjYiIHk9IjE3LjIiIHdpZHRoPSI5LjciIGhlaWdodD0iMi44IiByeD0iMS40Ii8%2BPC9zdmc%2B&logoColor=white)](https://github.com/nao1215/atago)
[![Go Reference](https://pkg.go.dev/badge/github.com/nao1215/jsonize.svg)](https://pkg.go.dev/github.com/nao1215/jsonize)
![GitHub](https://img.shields.io/github/license/nao1215/jsonize)

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

The output of more than two hundred commands is identified from its
text. Each format is a YAML definition, so reading another one needs no
Go and no rebuild. jz is one binary with no dependencies outside the Go
standard library.

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

## Supported OS (tested on GitHub Actions)

- Linux
- macOS
- Windows
- FreeBSD (the end-to-end suite, in a virtual machine; install with `go install`)

OpenBSD and NetBSD are built and linted, not run.

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
      --stop-on-error           end a stream at the first record that cannot be read
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

Piped output is identified from its text, and a format too generic to
claim is refused with the parser name to pass. `--extract` and
`--exclude` keep or drop keys of the objects jz prints; a key the output
cannot have is an error that lists the keys it has.

```console
$ df -h | jz --extract filesystem --extract mounted_on
[{"filesystem":"/dev/nvme0n1p2","mounted_on":"/"}, ...]
```

`jz run` hands the command your standard input, passes its standard
error through, mirrors its exit status and runs it with `LC_ALL=C`.
Everything after the command name belongs to the command, and a bare
`--` states that boundary explicitly.

### Files and text

A data file is read as the format its extension names (`.csv`, `.tsv`,
`.ltsv`, `.jsonl` or `.ndjson`, `.json`, `.yaml` or `.yml`, each also as
`.gz` or `.bz2`), and piped data as the format `--format` names, with no
detection; text the format does not allow is exit 3 with the line.
`--format text`, `lines` and `nul` read plain text as one string or a
list of strings, and `--type COLUMN=int` converts a CSV column.

```console
$ git branch --format='%(refname:short)' | jz --format lines
["main","feature/login"]

$ kubectl get deploy api -o yaml | jz --format yaml --extract spec
{"spec":{"replicas":3, ...}}
```

### JSON from arguments

`jz new` makes JSON from arguments. `=` makes a string, `:=` reads JSON,
`@path` reads a file and `[]` appends to an array. Nothing is guessed.
`--string` writes text as it is, `--text-file` keeps a file's line
endings, and `--path` places a value at a JSON Pointer.

```console
$ jz new name=api replicas:=3 'tags[]=web' spec:=@deploy.yaml
{"name":"api","replicas":3,"tags":["web"],"spec":{"replicas":3}}
```

### Streams

`--stream` answers a command that keeps printing with one JSON document
per line as each record is read, and `jz new --each` wraps each one as it
comes. A record that cannot be read is reported and left out, with exit
status 3; `--stop-on-error` ends the stream there instead.

```console
$ vmstat 1 | jz --stream | jz new --each --string host=web sample:=@-
{"host":"web","sample":{"r":1,"b":0,"swpd":0,"free":5123456,...}}
```

The [usage guide](https://nao1215.github.io/jsonize/usage/#reading-a-command-that-keeps-printing)
has the details, and `--raw`, `--explain` and `--parser` for looking at
how a definition reads a command's output.

## Detection and limits

Some formats need a parser name because their text is too generic to
identify automatically:

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

Naming a parser is a claim about the input, not a way past the checks:
the definition's signature still has to fit, and there is no option that
turns that off.

Which forms of which commands are read, and which are refused and why,
are listed at https://nao1215.github.io/jsonize/coverage/.

jz builds and runs on Windows, which is a different thing from reading
what Windows commands print. `ipconfig`, `ipconfig /all` and
`systeminfo` are read, and CI runs them on English Windows Server 2022
and 2025; other display languages and code pages are not checked.

## Adding a parser

Add a YAML definition, a captured `.txt` fixture and its `.yaml` capture
metadata, then run `jz test --update` to generate its expected JSON. Review that output and
run `jz test` to validate the definition against its fixtures and the
official fixture corpus.

Registries are layered: `JSONIZE_REGISTRY_PATH`, then the user registry
under the config directory, then the one built into the binary. Conversion
uses local definitions and does not access the network. The guide is at
https://nao1215.github.io/jsonize/write-a-parser/ and the reference at
https://nao1215.github.io/jsonize/definition-format/.

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

Diagnostics go to standard error. By default, reading finishes before
JSON is written, so a failure leaves standard output empty. jz writes
JSON only. With `--stream` and `jz new --each`, records already written
remain when a later record fails; check the exit status for skipped
records.

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
