---
title: Cookbook
description: Short recipes for jz, indexed by task, from converting a command's output to building a JSON payload in CI.
toc: true
filter: true
---

Each recipe is a command you can copy and the output it prints. Every
recipe on this page is run by the end-to-end suite
(`e2e/atago/cookbook.atago.yaml`) on Linux, macOS, Windows and FreeBSD,
so the output shown is what jz prints.

| I want to | Go to |
|-----------|-------|
| turn a command's output into JSON | [Convert what a command printed](#convert-what-a-command-printed), [Let jz run the command](#let-jz-run-the-command) |
| shape what comes out | [Keep only the keys you need](#keep-only-the-keys-you-need) |
| read a command that never stops | [Follow a command that keeps printing](#follow-a-command-that-keeps-printing) |
| understand or override detection | [See which parser jz chose](#see-which-parser-jz-chose), [Name the parser when detection refuses](#name-the-parser-when-detection-refuses), [Read output jz has no parser for](#read-output-jz-has-no-parser-for) |
| convert a data file | [Convert a CSV file](#convert-a-csv-file), [Read a CSV without a header line](#read-a-csv-without-a-header-line), [Convert YAML to JSON](#convert-yaml-to-json), [Read compressed JSON Lines logs](#read-compressed-json-lines-logs), [Read LTSV access logs](#read-ltsv-access-logs) |
| make JSON in a script | [Build a JSON body for an API call](#build-a-json-body-for-an-api-call), [Pass a variable that may start with @](#pass-a-variable-that-may-start-with-), [Put a file's contents into JSON](#put-a-files-contents-into-json), [Keep a file's line endings](#keep-a-files-line-endings), [Build nested JSON](#build-nested-json), [Make a JSON array](#make-a-json-array) |
| use jz in CI | [Fail a CI step when jz cannot read the output](#fail-a-ci-step-when-jz-cannot-read-the-output), [Get the JSON Schema of an output](#get-the-json-schema-of-an-output) |
| read a format jz does not know | [Add a parser of your own](#add-a-parser-of-your-own) |

## Convert what a command printed

Pipe the output to `jz`. The format is identified from the text.

```console
$ df -h | jz
[{"filesystem":"/dev/nvme0n1p2","size":"1.8T","used":"1.6T","available":"145G","use_percent":92,"mounted_on":"/"},{"filesystem":"tmpfs","size":"7.8G","used":"4.0K","available":"7.8G","use_percent":1,"mounted_on":"/tmp"}]
```

A rounded size such as `1.8T` stays a string; `use_percent` is exact, so
it is a number.

## Let jz run the command

`jz run` runs the command with `LC_ALL=C`, reads its standard output and
exits with the command's status. The command name also narrows the
parsers jz considers.

```console
$ jz run df -h
[{"filesystem":"/dev/nvme0n1p2","size":"1.8T", ...}]
```

## Keep only the keys you need

`--extract` keeps the named keys and `--exclude` drops them. A key the
output does not have is exit 2, so a typo cannot pass as an empty result.

```console
$ df -h | jz --extract mounted_on --extract use_percent
[{"use_percent":92,"mounted_on":"/"},{"use_percent":1,"mounted_on":"/tmp"}]
```

## Follow a command that keeps printing

`--stream` writes one JSON document per record as soon as the record is
read, which suits `vmstat 1`, `iostat 1` or `ping`.

```console
$ vmstat 1 | jz --stream
{"r":1,"b":0,"swpd":1950392,"free":7552816,"buff":2853928,"cache":39453912,"si":4,"so":16,"bi":511,"bo":1601,"in":20347,"cs":9,"us":3,"sy":1,"id":96,"wa":0,"st":0,"gu":0}
{"r":0,"b":0,"swpd":1950392,"free":7560728,"buff":2853928,"cache":39453924,"si":0,"so":0,"bi":0,"bo":60,"in":9188,"cs":12769,"us":1,"sy":0,"id":99,"wa":0,"st":0,"gu":0}
```

## See which parser jz chose

`--explain` writes the chosen definition and why on standard error, on
lines that start with `jz: explain:`. `--explain=json` writes the same
facts as one JSON document.

```console
$ df -h | jz --explain > /dev/null
jz: explain: chose df/gnu-human from embedded
jz: explain: scope: every definition in the registry, by its signature alone
...
```

## Name the parser when detection refuses

Some formats are too plain to identify on sight. jz refuses them with exit
4 and says which parser to name.

```console
$ git diff --numstat | jz
jz: unable to identify the input format
it could be `git` output, but that format is too generic for jz to claim on its own
...
$ git diff --numstat | jz --parser git
[{"added":10,"deleted":2,"path":"main.go"},{"added":3,"deleted":0,"path":"README.md"}]
```

## Read output jz has no parser for

`--define` takes the parse rules inline. Nothing is detected, so nothing
can be detected wrongly.

```console
$ mytool list | jz --define 'parse: {type: table}'
[{"name":"api","status":"Running","age":"3d"},{"name":"worker","status":"Pending","age":"5m"}]
```

## Convert a CSV file

A data file is read as the format its extension names: `.csv`, `.tsv`,
`.ltsv`, `.jsonl`, `.json`, `.yaml`, each also as `.gz` or `.bz2`.

```console
$ jz --file users.csv
[{"id":"1","name":"alice","email":"alice@example.com"},{"id":"2","name":"bob","email":"bob@example.com"}]
```

Every CSV value is a string. A row with more fields than the header is
exit 3 with the line.

## Read a CSV without a header line

```console
$ jz --file rows.csv --columns id,name
[{"id":"1","name":"alice"},{"id":"2","name":"bob"}]
```

## Convert YAML to JSON

```console
$ jz --file deploy.yaml
{"name":"api","replicas":3,"image":{"repository":"example/api","tag":"1.4"}}
$ cat deploy.yaml | jz --format yaml
{"name":"api","replicas":3,"image":{"repository":"example/api","tag":"1.4"}}
```

A bare `3` is a number and a quoted `"1.4"` a string, as YAML 1.2 types
them. Input from a pipe needs `--format`.

## Read compressed JSON Lines logs

```console
$ jz --file events.jsonl.gz --stream --extract msg
{"msg":"started"}
{"msg":"slow"}
```

## Read LTSV access logs

```console
$ jz --file access.ltsv
[{"time":"2026-09-15T07:00:01Z","host":"192.0.2.10","status":"200","req":"GET /api HTTP/1.1"},{"time":"2026-09-15T07:00:02Z","host":"192.0.2.11","status":"404","req":"GET /missing HTTP/1.1"}]
```

## Build a JSON body for an API call

`jz new` makes an object from its arguments: `=` is a string, `:=` is
JSON, `[]` appends to an array.

```console
$ jz new name=api version=1.4.0 replicas:=3 tags[]=web tags[]=prod
{"name":"api","version":"1.4.0","replicas":3,"tags":["web","prod"]}
$ jz new name=api replicas:=3 | curl -sS -H 'Content-Type: application/json' -d @- https://api.example.com/deploy
```

Nothing is guessed: `version=007` stays the string `"007"`.

## Pass a variable that may start with @

A plain `key=@x` reads the file `x`. `--string` writes the text as it
is. Quote the whole argument in the shell (sh, bash, zsh shown).

```console
$ MESSAGE='@here: deploy := done'
$ jz new --string "message=$MESSAGE" status=ok
{"message":"@here: deploy := done","status":"ok"}
```

## Put a file's contents into JSON

`=@path` reads a file's text, without its last newline. `:=@path` reads a
data file as the format its extension names. `@-` is standard input.

```console
$ jz new version=@VERSION spec:=@deploy.yaml
{"version":"1.4.0","spec":{"name":"api","replicas":3,"image":{"repository":"example/api","tag":"1.4"}}}
```

## Keep a file's line endings

`=@path` drops the last line ending; `--text-file` keeps the text whole.

```console
$ jz new --text-file body=NOTES.txt title=@TITLE
{"body":"Fixed a crash.\n\nThanks to everyone.\n","title":"Release 1.4"}
```

## Build nested JSON

`--path` takes a JSON Pointer. `-` appends to an array.

```console
$ jz new --path /metadata/name=api --path /spec/replicas:=3 --path /spec/containers/-/image=example/api:1.4 kind=Deployment
{"metadata":{"name":"api"},"spec":{"replicas":3,"containers":[{"image":"example/api:1.4"}]},"kind":"Deployment"}
```

A location given twice is refused instead of overwritten.

## Make a JSON array

```console
$ jz new --array web :=8080 :=true
["web",8080,true]
```

## Fail a CI step when jz cannot read the output

jz prints complete JSON or nothing. Its exit status says why:

| Exit | Meaning |
|------|---------|
| 0 | converted |
| 1 | an error such as an unreadable file |
| 2 | a usage error |
| 3 | the input does not fit the chosen format |
| 4 | the format could not be identified, or more than one fits |
| 5 | a registry could not be loaded |

`jz run` exits with the command's own status when the command fails.

```console
$ printf 'no format here\n' | jz
jz: unable to identify the input format
...
$ echo $?
4
```

## Get the JSON Schema of an output

Every definition publishes the schema of what it produces, to validate
the JSON in a later step.

```console
$ jz list --schema df gnu-human
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://nao1215.github.io/jsonize/schemas/df/gnu-human.json",
  ...
```

## Add a parser of your own

A parser is a YAML file and a captured output. Put them in a directory
and point `JSONIZE_REGISTRY_PATH` at it.

```text
my-registry/parsers/mytool/status/parser.yaml
my-registry/parsers/mytool/status/testdata/basic.txt
```

```yaml
format: 1
command: mytool
variant: status
description: mytool status, one service per line
detect:
  signature:
    all:
      - '\ASERVICE[ \t]+STATE[ \t]*$'
parse:
  type: table
fields:
  service: {required: true}
```

```console
$ jz test --update my-registry
golden files written under /home/you/my-registry
1 passed, 0 failed
$ export JSONIZE_REGISTRY_PATH=$PWD/my-registry
$ mytool status | jz
[{"service":"api","state":"running"},{"service":"worker","state":"stopped"}]
```

`jz test` checks every fixture against its golden JSON and that no other
definition reads it. [Write a parser](../write-a-parser/) covers the rest.
