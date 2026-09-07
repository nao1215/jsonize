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
```

Options:

```text
  -f, --file PATH     read input from PATH instead of stdin
  -p, --pretty        indent JSON output
      --extract KEY   keep only this key (repeatable)
      --exclude KEY   drop this key (repeatable)
      --parser NAME   restrict detection to one parser
      --variant NAME  use a variant of --parser
  -h, --help          show help
