---
description: jsonize (jz) makes JSON for the next command in a pipeline from command output, data and text files, and command-line arguments. It refuses what it cannot identify and needs no dependencies.
---

jsonize makes JSON for the next command. `jz` turns what a command
printed, a CSV, YAML or text file, or the arguments of a script into
JSON and hands it on, to `jq`, `curl` or a CI step. When the input is not
what it should be, jz says so and prints nothing, so the next command
never reads a guess.

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
$ jz --file users.csv --type id=int
$ jz new --string "message=$MESSAGE" --path /metadata/labels/app=api replicas:=3
```

## Why jsonize?

| You want | Use |
|----------|-----|
| JSON from a command that has no `--json` option | `COMMAND \| jz` or `jz run COMMAND` |
| a CSV, TSV, LTSV, JSON Lines or YAML file as JSON | `jz --file FILE` |
| lines, or the names `find -print0` prints, as a JSON array | `COMMAND \| jz --format lines` (or `nul`) |
| a JSON body built in a shell script | `jz new key=value key:=json` |
| every record of a stream wrapped with the fields a consumer needs | `jz --stream \| jz new --each host=web sample:=@-` |
| to select, sort or reshape JSON you already have | [jq](https://jqlang.org/) |
| a parser for a command jz does not know yet | a YAML definition in your own registry |

jz reads 241 commands through 686 definitions, from `df`, `ps` and `ip`
to `kubectl get`, `docker ps`, `tasklist` and `diskutil list`, on Linux,
macOS, Windows and FreeBSD. A definition is a YAML file with captured
output beside it; no Go code is needed to add one.

## Data files and text

A file is read as the format its extension names, and piped data as the
format `--format` names. Nothing is detected, a CSV column stays text
unless `--type` names its type, and text the format does not allow is
exit 3 with the line. `--format lines` and `nul` read plain text as a
list of strings.

![jz reading a CSV file and a YAML file](demo/datafile.gif)

## JSON from arguments

`jz new` builds a document from `key=value` (a string), `key:=json` (any
JSON value), `key=@file` (a file's text) and `key[]=value` (an array).
`--string` passes text that must not be read as a file name, `--path`
places a value at a JSON Pointer, and `--each` makes one document per
line of standard input. Nothing is guessed from a value.

![jz new building objects and an array](demo/new.gif)

## Install

```sh
go install github.com/nao1215/jsonize/cmd/jz@latest
```

Homebrew, release archives, Linux packages and shell completion are on
the [install page](install/).

## Where to go

- [Cookbook](cookbook/): copyable recipes by task, each run by the test suite.
- [Usage](usage/): every source of JSON, every option, streams and the exit codes.
- [Parsers](parsers/): the commands and variants jz reads, with their JSON Schemas.
- [Write a parser](write-a-parser/): add a definition for a command jz does not know.
- [Definition format](definition-format/): the reference for the YAML.
- [Design](design/): why jz refuses rather than guesses.
