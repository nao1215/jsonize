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
| read a command that never stops | [Follow a command that keeps printing](#follow-a-command-that-keeps-printing), [Add fields to every record of a stream](#add-fields-to-every-record-of-a-stream), [Keep what a stream wrote before a bad record](#keep-what-a-stream-wrote-before-a-bad-record) |
| understand or override detection | [See which parser jz chose](#see-which-parser-jz-chose), [Name the parser when detection refuses](#name-the-parser-when-detection-refuses), [Read output jz has no parser for](#read-output-jz-has-no-parser-for) |
| convert a data file | [Convert a CSV file](#convert-a-csv-file), [Type the columns of a CSV file](#type-the-columns-of-a-csv-file), [Read a CSV without a header line](#read-a-csv-without-a-header-line), [Convert YAML to JSON](#convert-yaml-to-json), [Read compressed JSON Lines logs](#read-compressed-json-lines-logs), [Read LTSV access logs](#read-ltsv-access-logs) |
| turn plain text into JSON | [Turn lines into a JSON array](#turn-lines-into-a-json-array), [Read find -print0 output](#read-find--print0-output), [Wrap a whole text in a JSON string](#wrap-a-whole-text-in-a-json-string) |
| make JSON in a script | [Build a JSON body for an API call](#build-a-json-body-for-an-api-call), [Pass a variable that may start with @](#pass-a-variable-that-may-start-with-), [Put a file's contents into JSON](#put-a-files-contents-into-json), [Keep a file's line endings](#keep-a-files-line-endings), [Build nested JSON](#build-nested-json), [Make a JSON array](#make-a-json-array) |
| hand the JSON to jq | [Pick the records you want with jq](#pick-the-records-you-want-with-jq), [Fail a step when a value is out of range](#fail-a-step-when-a-value-is-out-of-range) |
| send or store the JSON | [Send JSON to an HTTP API](#send-json-to-an-http-api), [Save a report for a later step](#save-a-report-for-a-later-step) |
| use jz in CI | [Fail a CI step when jz cannot read the output](#fail-a-ci-step-when-jz-cannot-read-the-output), [Read the explanation in a script](#read-the-explanation-in-a-script), [Get the JSON Schema of an output](#get-the-json-schema-of-an-output) |
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

## Add fields to every record of a stream

`jz new --each` wraps each JSON Lines record as it arrives. `:=@-` is the
record.

```console
$ vmstat 1 | jz --stream | jz new --each --string host=server-a sample:=@-
{"host":"server-a","sample":{"r":1,"b":0}}
{"host":"server-a","sample":{"r":0,"b":0}}
```

## Keep what a stream wrote before a bad record

A stream ends at the first record it cannot read and exits 3. The records
written before it are already out and stand; the ones after it are not
read, so a consumer never gets a stream with a silent gap in it.

```console
$ printf '{"n":1}\nnot json\n{"n":2}\n' | jz --format jsonl --stream
{"n":1}
jz: jsonl: line 2: the line is not one JSON value
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

## Type the columns of a CSV file

Every value of a CSV is text until `--type` names its type. An empty
value is null.

```console
$ jz --file sales.csv --type units=int --type price=float --type in_stock=bool
[{"sku":"007","units":12,"price":1.5,"in_stock":true},{"sku":"008","units":null,"price":null,"in_stock":false}]
```

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

## Turn lines into a JSON array

Each line is a string; empty lines and spaces stay.

```console
$ git branch --format='%(refname:short)' | jz --format lines
["main","feature/login"," spaced "]
```

## Read find -print0 output

A NUL ends each record, so a name with a space or a line break is one
string.

```console
$ find . -name '*.log' -print0 | jz --format nul
["./a.log","./b c.log","./line\nbreak.log"]
```

## Wrap a whole text in a JSON string

```console
$ git log -1 --format=%B | jz --format text
"Fix the parser\n\nIt dropped the last line.\n\n"
```

## Build a JSON body for an API call

`jz new` makes an object from its arguments: `=` is a string, `:=` is
JSON, `[]` appends to an array.

```console
$ jz new name=api version=1.4.0 replicas:=3 'tags[]=web' 'tags[]=prod'
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

## Pick the records you want with jq

jz does not select, sort or compute. It makes the JSON, and `jq` takes it
from there, which is why the keys are named and typed rather than left as
the text the command printed.

```console
$ df -h | jz | jq -r '.[] | select(.use_percent >= 90) | .mounted_on'
/
```

`use_percent` is a number because df prints an exact one, so `>= 90` is a
comparison rather than string work. A rounded size (`1.8T`) is a string,
and a threshold on it belongs to the tool that knows what T means.

## Fail a step when a value is out of range

`jq -e` exits 1 when its result is false or null, so a check is one
pipeline and the shell's own status.

```console
$ df -h | jz | jq -e 'all(.use_percent < 90)' > /dev/null
$ echo $?
1
```

Put `set -o pipefail` around it, or jz failing to read the output is
hidden by jq succeeding on an empty input.

## Send JSON to an HTTP API

`jz new` writes the body and curl reads it from standard input, so the
JSON is never a string the shell has to quote.

```console
$ jz new service=api replicas:=3 | curl -sS -X POST -H 'content-type: application/json' --data-binary @- http://127.0.0.1:8080/deploy -o /dev/null -w '%{http_code}\n'
200
```

`--data-binary @-` rather than `-d @-`: the second strips line endings,
which matters when a value came from a file. Put `set -o pipefail` around
the pipeline, or jz failing to make the body is hidden by curl posting
nothing successfully.

## Save a report for a later step

jz writes complete JSON or nothing, so a file it wrote is a file a later
step can read. Redirect its output and check the status before the file
is used.

```console
$ jz run df -h > df.json
$ jq -r '.[0].mounted_on' df.json
/
```

A failure leaves the file empty and the status non-zero; `set -e` or an
explicit check is what stops the later step.

## Read the explanation in a script

`--explain=json` writes one JSON document to standard error, which leaves
standard output for the conversion. Every line it writes starts with
`jz: explain: `, so a script cuts that off and reads the rest.

```console
$ df -h | jz --explain=json 2>&1 >/dev/null | sed -n 's/^jz: explain: //p' | jq -r '.chosen.definition'
df/gnu-human
```

The document says `outcome`, what the choice was made from (`scope`),
what was chosen and what matched (`chosen`), what was turned down
(`rejected`), what was read (`read`), and, when the conversion failed,
`error` with the exit status jz ended with.

```console
$ printf 'no format here\n' | jz --explain=json 2>&1 >/dev/null | sed -n 's/^jz: explain: //p' | jq -c '{outcome, exit: .error.exit}'
{"outcome":"unidentified","exit":4}
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
