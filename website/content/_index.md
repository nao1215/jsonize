---
description: jsonize (jz) turns command output, data files and command-line arguments into JSON. It detects the format from the text, refuses what it cannot identify, and needs no dependencies.
---

jsonize turns what a terminal prints into JSON. Pipe a command to `jz`
and the format is identified from the text; read a CSV, YAML or JSON
Lines file by its extension; or build a JSON document from arguments.
When the input is not a format jz knows, it says so and prints nothing,
so a script never reads a guess.

![jz reading df, uptime and free, and refusing input it cannot identify](demo/jsonize.gif)

## Try it in 30 seconds

```console
$ df -h | go run github.com/nao1215/jsonize/cmd/jz@latest
[{"filesystem":"/dev/nvme0n1p2","size":"1.8T","used":"1.6T","available":"145G","use_percent":92,"mounted_on":"/"}, ...]
```

A size rounded by `-h` stays the string `df` printed, and `use_percent`
is a number.

## Three things to try next

```console
$ jz run --pretty free
$ jz --file users.csv
$ jz new name=api replicas:=3 tags[]=web
```

## Why jsonize?

| You want | Use |
|----------|-----|
| JSON from a command that has no `--json` option | `COMMAND \| jz` or `jz run COMMAND` |
| to query or reshape JSON you already have | [jq](https://jqlang.org/) |
| a CSV, TSV, LTSV, JSON Lines or YAML file as JSON | `jz --file FILE` |
| a JSON body built in a shell script | `jz new key=value key:=json` |
| a parser for a command jz does not know yet | a YAML definition in your own registry |

jz reads 224 commands through 596 definitions, from `df`, `ps` and `ip`
to `kubectl get`, `docker ps`, `tasklist` and `diskutil list`, on Linux,
macOS, Windows and FreeBSD. A definition is a YAML file with captured
output beside it; no Go code is needed to add one.

## Data files

A file is read as the format its extension names. Piped text is detected
as command output unless `--format` names a data format. Nothing is
detected for a data file, and text the format does not allow is exit 3
with the line.

![jz reading a CSV file and a YAML file](demo/datafile.gif)

## JSON from arguments

`jz new` builds an object from `key=value` (a string), `key:=json` (any
JSON value), `key=@file` (a file's text) and `key[]=value` (an array).
Nothing is guessed from a value.

![jz new building objects and an array](demo/new.gif)

## Install

```sh
go install github.com/nao1215/jsonize/cmd/jz@latest
```

Homebrew, release archives, Linux packages and shell completion are on
the [install page](install/).

## Where to go

- [Cookbook](cookbook/): copyable recipes by task, each run by the test suite.
- [Usage](usage/): every option, `--stream`, `--explain` and the exit codes.
- [Parsers](parsers/): the commands and variants jz reads, with their JSON Schemas.
- [Write a parser](write-a-parser/): add a definition for a command jz does not know.
- [Definition format](definition-format/): the reference for the YAML.
- [Design](design/): why jz refuses rather than guesses.
