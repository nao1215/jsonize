---
title: Usage
description: The two ways to give jz input, the options it takes, and what its exit codes mean.
toc: true
---

## Two ways in

```console
COMMAND | jz                 # convert piped output
jz < captured.txt            # or output captured earlier
jz --file captured.txt
jz --file users.csv          # a data file, read as the format its extension names
COMMAND | jz --format yaml   # piped data, read as the format --format names
jz run COMMAND [args...]     # let jz run the command and convert its stdout
jz new KEY=VALUE KEY:=JSON   # make JSON from arguments
```

The mode is always explicit, so a pipeline behaves the same in a shell
and in CI; only `jz` with no arguments at a terminal prints help.

`jz run` hands the command your standard input, passes its standard error
through, mirrors its exit status, and runs it with `LC_ALL=C` so the
output is the one the parsers describe. A command that succeeds without
printing anything is answered with `[]`, or, when its parser's variants
read a single object, with a message that jz cannot tell. Empty piped
input has no answer, since nothing identifies it.

## Options

```text
  -f, --file PATH               read input from PATH instead of stdin
      --format NAME             read the input as this format: csv, tsv, ltsv, jsonl, json, yaml, text, lines, nul
  -p, --pretty                  indent JSON output
      --stream                  write each record as soon as it is read
      --raw                     skip the field rules and report every value as text
      --extract KEY             keep only this key (repeatable)
      --exclude KEY             drop this key (repeatable)
      --assume-year YEAR        date the timestamps a format prints without a year (or "now")
      --assume-zone ABBR=+HHMM  give a zone abbreviation an offset (repeatable)
      --parser NAME             restrict detection to one parser
      --variant NAME            use a variant of --parser
      --define YAML             read with a definition given here instead of a registered one
      --columns NAME,...        name the columns of a csv read without a header line
      --type COLUMN=TYPE        convert a csv or tsv column to int, float or bool (repeatable)
      --explain[=json]          report the chosen definition and why, on stderr
  -h, --help                    show help
```

`jz run` adds `--env NAME=VALUE`, `--keep-locale` and `--timeout`, which
control the command rather than the conversion. `jz list` adds `--json`,
`--schema` and `--sources`, and `jz test` adds `--update` and `--decoys`.
`jz completion bash|zsh` prints a shell completion script (see
[Install](../install/#shell-completion)).

Options that state two answers at once are refused with exit status 2
before the input is opened or a command is started: `--pretty` with
`--stream`, `--extract` with `--exclude`, `--define` with
`--parser`, `--format` with `--parser` or `--define`, and `--columns`
where there are no columns to name.

## The shape of the output

`jz list --schema` prints what a definition produces as a JSON Schema:
the keys, their types, which are always there, which may be null, and
the values a key can take when the definition names them.

```console
$ jz list --schema df gnu | jq -c '.items.properties.use_percent'
{"type":["integer","null"]}
```

The official schemas are at
`https://nao1215.github.io/jsonize/schemas/COMMAND/VARIANT.json`, with a
contract version in `x-jsonize.version` that goes up only for a
[breaking change](../parsers/#what-each-definition-produces).

## JSON is the only output

jz writes JSON, and with `--stream` one JSON document per line. YAML is
still read (a `.yaml` file, `--format yaml`, a definition), but `--yaml`
as an output option was removed and is refused with exit status 2. To get
YAML, hand the JSON to a YAML tool:

```console
$ df -h | jz | yq -P
```

## Choosing the keys

`--extract` keeps the keys it names and `--exclude` drops them. Both may
be repeated and apply to the top-level keys of every object jz prints; a
nested value keeps whatever it holds. They cannot be combined.

```console
$ df -h | jz --extract filesystem --extract mounted_on
[{"filesystem":"/dev/nvme0n1p2","mounted_on":"/"}, ...]

$ df | jz --extract mountpoint
jz: no key "mountpoint" in the output
the keys it has are "1k_blocks", "available", "filesystem", "mounted_on", "use_percent", "used"
```

A key is checked against what the definition lists in `jz list --schema`,
not against one input, so a key some records leave out, or any key of an
empty listing, narrows to nothing. Only a key taken from the input (a
column named by a header) is looked for in the result.

## Reading a command that keeps printing

`vmstat 1`, `iostat 5` and `tail -f` do not end. `--stream` writes one
JSON document per line, each as soon as its record is complete:

```console
$ jz run --stream vmstat 1 | jq -c 'select(.id < 50)'
$ iostat -x 5 | jz --stream
```

A table, csv, a regex matched per line, a key/value list, `records` and a
tree stream one record at a time; a composite streams one document per
part. A format read into one object (`each: input`, a kv map, an ini
file) is refused with `format <id> has no streaming form` and exit
status 2, and so is `--pretty`, which would spread a record over lines.

jz chooses a definition as soon as no line still to come could change
the choice. `vmstat 1` is decided by its two header lines, so `jz run
--stream vmstat 1` and `vmstat 1 | jz --stream` write the first sample as
soon as it is printed. The choice waits while a later line could still
decide it: an unanchored signature expression (`^Filesystem`, not
`\AFilesystem`) or a `none` expression that has not matched, an
expression on the end of the text (`\z`), or a rival that would win or
tie if it came to fit. The wait is at most the widest signature window in
scope (twenty lines by default, up to 200) or the end of the input, so
piped input can wait longer than `--parser` or `jz run`. A variant with
no signature (`--parser ls --variant names-zero`) waits for one record.
Blank lines before the text are not counted.

A table row or a regex line is complete at its line break. A `records`
block (`iostat`, the sysstat reports) is complete when the next block
starts or the input ends, so `iostat 1` writes a sample a second later.

`--extract` and `--exclude` apply to each record. A finished record is
not kept, so the limits bound one record, not the stream: 64 MiB for what
is held while a record waits for its end (a fold, a block, a quoted csv
value), 1 MiB for a single line, and 4,194,304 values.

### A composite as a stream

ping's header, replies and summary are one object read whole, and are
written part by part when streamed:

```console
$ jz run --stream ping -c 2 192.0.2.1          # macOS
{"part":"destination","value":{"name":"192.0.2.1","address":"192.0.2.1",...}}
{"part":"replies","value":{"kind":"no_answer","error":"Request timeout","icmp_seq":0}}
{"part":"statistics","value":{"name":"192.0.2.1","packets_transmitted":2,"packets_received":0,...}}
```

A list part (the replies) is one document per element, written when its
line is read. A single-value part is one document, written when all its
lines have come. `--extract` and `--exclude` name the parts:
`--extract replies` writes the reply documents only.

### A record that cannot be read

Without `--stream`, a record that does not fit means nothing is written
and the status is 3. In a stream, the record is reported on standard
error and the ones after it are still written:

```text
jz: du/posix: line 2: expected at least 2 fields but found 1: "garbage"
```

The status is 3 at the end if anything was skipped. Two failures still
end a stream at once: text whose format cannot be identified (exit 4,
settled on the leading lines before any record is written) and a format
with no streaming form (exit 2).

When `jz run` has a failing command and a skipped record, it returns the
command's status. The skipped records are on standard error either way.

### Watching a stream for the records it left out

`--explain=json` reports each record the stream leaves out as it happens,
on its own line:

```console
$ printf 'root:x:0:0:root:/root:/bin/bash\nbroken\n' | jz --stream --explain=json --parser etc --variant passwd 2>&1 >/dev/null | sed -n 's/^jz: explain: //p'
{"outcome":"chosen","scope":{"from":"--parser","parser":"etc","variant":"passwd",...},...}
{"event":"skipped","definition":"etc/passwd","line":2,"reason":"expected at least 7 fields but found 1: \"broken\"","skipped":1}
```

The explanation has an `outcome` key; an event has `event`, which is
`skipped`. `definition` is the definition the stream reads with, `line`
the input line of the failure (`null` when it is not about one line),
`reason` the error without the definition and line in front, cut at
1,024 bytes and ended with `...` when cut, and `skipped` the records left
out so far, this one included. `--explain` writes the same fact as text,
after the ordinary diagnostic:

```text
jz: explain: skipped: a record of etc/passwd at line 2 (1 record so far): expected at least 7 fields but found 1: "broken"
```

Each explain line is a single write. In `jz run` a command that leaves a
line unended can put its text in front of it; `sed -n 's/.*jz: explain:
//p'` still finds the document.

### A command ended before it finished

With `--stream`, a command stopped by `--timeout` or a signal keeps the
records written before the cut and loses the one it fell in. Without it
nothing is written. The status is 128 plus the signal.

An interrupt or SIGTERM that jz receives is passed on to the command,
except Ctrl-C at a terminal: the terminal sends it to the whole
foreground process group, so jz does not send it again. The same holds
for `kill -INT` sent to jz alone in the foreground of a terminal; send it
to the process group (`kill -INT -- -PGID`), or send SIGTERM, which is
always passed on.

## Data files

A file is read as the format its extension names, and data from a pipe as
the format `--format` names; without `--format`, piped text is detected as
command output. Nothing is detected for a data file, and the text is held
to that format from the first line to the last.

| Format | Extensions | What jz writes |
|--------|------------|----------------|
| `csv` | `.csv` | a list of objects, one per row, keyed by the header line |
| `tsv` | `.tsv` | the same, with fields separated by tabs |
| `ltsv` | `.ltsv` | a list of objects, one per line, keyed by the labels |
| `jsonl` | `.jsonl`, `.ndjson` | a list of the values, one per line |
| `json` | `.json` | the document |
| `yaml` | `.yaml`, `.yml` | the document, as JSON |
| `text` | none | the whole input as one string |
| `lines` | none | a list of strings, one per line |
| `nul` | none | a list of strings, one per NUL-terminated record |

`text`, `lines` and `nul` have no extension, since a `.txt` file is
usually a command's output; name them with `--format`. The others may end
in `.gz` or `.bz2`. Extensions are matched in any
case. A file whose extension names no format (`captured.txt`) is detected
from its text.

```console
$ jz --file users.csv
[{"id":"1","name":"alice"},{"id":"2","name":"bob, jr"}]

$ jz --file events.jsonl.gz --stream --extract msg
{"msg":"started"}
{"msg":"slow"}

$ kubectl get deploy api -o yaml | jz --format yaml
```

`--format` wins over the extension, and `--parser` or `--define` read the
file as a command's output.

- csv and tsv are read by the [csv shapes](#a-csv-without-a-header-line):
  every value is text, an empty field is `""`, a row with more fields
  than the header is refused, and `--columns` names the columns of a
  file without a header line (`jz --file rows.csv --columns id,name`).
- An LTSV value is everything after the first colon. A label is letters,
  digits and `_ . -`; a missing label, an empty field and a label given
  twice on one line are refused.
- JSON Lines is one JSON value per line; blank lines are skipped.
- JSON keeps key order and the digits of numbers (`2.50` stays `2.50`). A
  key given twice in one object, text after the document and nesting
  deeper than 1000 are refused.
- YAML is typed by the YAML 1.2 core schema: `null` and `~`, `true` and
  `false`, integers (with `0x` and `0o`) and decimals; anything quoted
  is a string. `.inf`, `.nan`, anchors, aliases, tags and a second
  document are refused.
- Text that is not UTF-8 is refused, and a leading byte order mark is not
  part of the text, except in `nul`, which keeps every byte.

A reading that stops is exit 3 and names the line (`jz: json: line 3: the
key "a" is given twice in one object`), as is a file that cannot be
decompressed. `--stream` writes the records of `jsonl`, `ltsv`, `lines`
and `nul` as they are read, and is a usage error with `json`, `yaml` and
`text`. `--explain` says which format was read and what named it.

### Text, lines and NUL-separated records

For output that has no format of its own, jz reads the text as strings.
Nothing is trimmed, spaces and empty lines included.

```console
$ printf 'first\r\n\n  spaced  \nno ending' | jz --format lines
["first","","  spaced  ","no ending"]
$ find . -name '*.log' -print0 | jz --format nul
["./a.log","./b c.log"]
$ git log -1 --format=%B | jz --format text
"Fix the parser\n\nIt dropped the last line.\n\n"
```

- `text` is the input as it is, an empty input `""`.
- A line ends with LF or CRLF, and the ending is not part of it; a lone
  CR is. A line ending at the end of the input ends the last line rather
  than starting an empty one, and a last line without one is still a
  line. An empty input is `[]`, an empty line `""`.
- A `nul` record ends with a NUL, the way `find -print0` and `xargs -0`
  write them, and keeps every other byte, line breaks included. A NUL at
  the end ends the last record, and two in a row make an empty record.
- A record that is not UTF-8 is exit 3, with its line (`lines: line 2`)
  or its number (`nul: record 2`).

### Typing the columns of a CSV

A csv or tsv keeps every value as the text it was. `--type COLUMN=TYPE`
converts one column, and may be repeated:

```console
$ jz --file sales.csv --type units=int --type price=float --type in_stock=bool
[{"sku":"007","units":12,"price":1.5,"in_stock":true},{"sku":"008","units":null,"price":null,"in_stock":false}]
```

- The types are `int`, `float` and `bool`, converted the way a
  definition's field of that type is: an integer with an optional sign, a
  decimal (not `NaN` or an infinity), and `true`, `yes`, `on`, `1`, `y` or
  `false`, `no`, `off`, `0`, `n` in any case. Space around a value is
  not part of it.
- An empty value is `null`. A column not named keeps its text, so `007`
  stays `"007"`.
- A value that is not the type is exit 3 with the line and the column
  (`csv/comma: line 3: field "units": cannot convert "four" to int`);
  with `--stream` the rows around it are written and the status is 3.
- A column is named as it appears in the output: the header normalised
  (`In Stock` is `in_stock`), a `--columns` name, or `column_2`. A column
  the header line (or the first record, for numbered columns) does not
  have is exit 2 before any row is written, and so is a column named
  twice or an unknown type. An empty input has no columns and is `[]`.
- `--type` with `--raw` is refused: `--raw` leaves out every field rule,
  and a type is one.
- `--type` applies to a `.csv` or `.tsv` file, `--format csv` or `tsv`,
  and `--parser csv --variant ...`, also with `jz run`. A `--define`
  states its own fields.

## Making JSON from arguments

`jz new` prints a JSON object made of its arguments, or an array with
`--array`.

```console
$ jz new name=api replicas:=3 debug:=false tags[]=web tags[]=prod
{"name":"api","replicas":3,"debug":false,"tags":["web","prod"]}

$ jz new version=@VERSION spec:=@deploy.yaml
{"version":"1.4.0","spec":{"replicas":3,"image":"1.2"}}

$ jz new --array web :=1 :=null
["web",1,null]
```

| Argument | Value |
|----------|-------|
| `key=text` | the string `text` |
| `key:=json` | the JSON `json`: a number, `true`, `false`, `null`, `"a string"`, `[...]`, `{...}` |
| `key=@path` | the text of the file, less its last line ending, as a string |
| `key:=@path` | the value the file holds, read as the format its extension names; JSON when it names none |
| `key[]=...` | one more element of the array `key`, with any of the forms above |

- A path of `-` is standard input, which one argument may read.
- With `--array` a word is a string, and `=`, `:=`, `=@` and `:=@` in
  front of a value mean what they mean after a key.
- Nothing is guessed: `version=007` is `"007"`; use `:=` for numbers.
- A key given twice is refused unless it ends in `[]`, and so is a key
  given both with and without `[]`.
- The key is everything before the first `=`, so `spec.replicas:=3` is
  the key `spec.replicas`; nesting is `--path`.
- Options come before the arguments; `--` ends them, so `jz new -- -x=1`
  has the key `-x`. `-p` works as elsewhere.

### Text that has to stay text

A plain argument reads its value: `note=@here` names the file `here`.
`--string KEY=TEXT` writes TEXT as it is, so a value from a variable
cannot become a file name or JSON, whatever it starts with. The text is
everything after the first `=`, spaces, quotes, line breaks and `--`
included.

```console
$ jz new --string note=@here --string 'rule=a := b' --string flag=--pretty
{"note":"@here","rule":"a := b","flag":"--pretty"}
```

In a POSIX shell (sh, bash, zsh) quote the whole argument:
`jz new --string "message=$MESSAGE"`; in PowerShell,
`jz new --string "message=$env:MESSAGE"`. With `--array`, leave the key
empty: `--string =@here`.

### A file's text with its line endings

`KEY=@FILE` drops the one line ending a text file ends with.
`--text-file KEY=PATH` keeps the text whole: every line ending, CRLF or
LF, an empty file as `""`. `-` is standard input. Text that is not UTF-8
is refused with exit 3, not replaced.

```console
$ printf 'Fixed a crash.\r\n\r\n' | jz new --text-file notes=- version=@VERSION
{"notes":"Fixed a crash.\r\n\r\n","version":"1.4.0"}
```

### Nested values

`--path POINTER=VALUE` places a value at a JSON Pointer (RFC 6901), with
the same operators as a plain argument: `=`, `:=`, `=@` and `:=@`.
`--string` and `--text-file` take a pointer in place of a key when it
starts with `/`.

```console
$ jz new --path /metadata/name=api --path /spec/replicas:=3 --path /spec/ports/-:=80 kind=Deployment
{"metadata":{"name":"api"},"spec":{"replicas":3,"ports":[80]},"kind":"Deployment"}
```

- The objects on the way are made as needed, in the order they are first
  named. A token is a key; `-` appends to an array; a number is the index
  of an element an earlier argument gave (`/items/-/name=a
  /items/0/port:=80`), and a key when the value there is an object.
- A new container is an array only when the next token is `-`. A number
  where nothing exists yet is refused rather than read as a key or as a
  position to pad up to.
- `~1` is `/` and `~0` is `~` inside a token. A pointer cannot name a key
  that holds `=`; write that key inside a `:=` value.
- A location is given once. A second value for it, a pointer into a value
  an argument gave whole (`spec:={...}`, `--string name=x`), a key in an
  array and `-` in an object are all refused, naming the argument in the
  way. Nothing is overwritten, retyped or padded.
- `--string`, `--text-file` and `--path` may be repeated and are placed
  in the order given, before the plain arguments.
- With `--array`, a pointer starts with `/-` or the index of an element
  already given.

A malformed argument (no `=`, an empty key, a location given twice, `:=`
with invalid JSON, a malformed pointer, two arguments reading standard
input) is exit 2 before anything is read. An unreadable file is exit 1,
and one that is not the format it is read as, or not UTF-8, is exit 3
with the line.

## Output jz has no definition for

`--define` takes a definition instead of the name of one: a `parser.yaml`
without `format`, `command`, `variant` and `detect`, so `parse` and,
optionally, `input` and `fields`.

```console
$ sqlite3 -box app.db 'select * from users' | jz --define 'parse: {type: table, split: box}'
$ jz --define 'parse: {type: csv, delimiter: "|"}' --file export.txt
$ mytool --list | jz --define '
    input: {select: {after: "^---"}}
    parse: {type: kv, separator: ": "}'
```

Nothing is detected, and it cannot be combined with `--parser` or
`--variant`. A body that cannot be read is exit 5 and the message names
the key (`--define: parse.type: unknown parse type "tabel"`).

The shape definitions do the same job under a name: `table`
(`whitespace`, `aligned`, `box`), `csv` (`comma`, `tab`,
`comma-no-header`, `tab-no-header`), `kv` (`colon`, `equals`) and `ini`
(`default`), as in `kubectl get nodes | jz --parser table --variant
whitespace`. Automatic detection never reaches them, and every value is
text.

### A csv without a header line

`csv/comma` and `csv/tab` take the first line for the column names. A
file with no such line (`sqlite3 -csv`, `mysql -B -N`) is read with the
`-no-header` variants:

```console
$ sqlite3 -csv app.db 'select id, name from users' | jz --parser csv --variant comma-no-header --columns id,name
[{"id":"1","name":"alice"},{"id":"2","name":"bob"}]
```

- Without `--columns` the columns are `column_1`, `column_2` and so on,
  as many as the first record has. A `--columns` name is letters, digits
  and `_ . : @ -`, and a name given twice is refused.
- A record with fewer fields than columns leaves the rest `null`; one
  with more is refused (exit 3).
- An empty field is `""`, quoted or not. A quoted value may hold the
  delimiter, a doubled quote or a line break.
- `--columns` works with a variant or a `--define` that reads a csv with
  no header line and no names of its own; anywhere else it is a usage
  error, reported before the input is opened.

## HTTP headers

`curl/headers` reads each header block `curl -I` or `curl -D -` prints as
a record:

```console
$ curl -sIL https://example.com/old | jz
[{"status":{"version":"1.1","code":301,"reason":"Moved Permanently"},"headers":[{"name":"Location","value":"https://example.com/new"}]},
 {"status":{"version":"2","code":200,"reason":null},"headers":[{"name":"set-cookie","value":"a=1"},{"name":"set-cookie","value":"b=2"}]}]
```

- A redirect followed with `-L`, a 1xx response and a proxy's reply to
  CONNECT are records of their own; the last one is the final response.
- `code` is an integer; `reason` is `null` when absent, as in HTTP/2.
- Headers are a list in the order they came, so a repeated header is two
  entries. Names keep their case, and values are text.
- `-v` writes to standard error, so `curl -sIv` reads as `curl -sI`. Its
  lines merged into the output (`--stderr -`) are refused, and so is a
  body after the headers (`curl -i`).

## Timestamps a format does not fully state

`who`, `last` and `journalctl -o short` print `Nov  4 13:17` with no
year, and `date`, `timedatectl` and `systemctl list-timers` name a zone
by an abbreviation. Both stay text unless you supply the missing half:

```console
$ who | jz --extract time
[{"time":"Nov  4 13:17"}]

$ who | jz --assume-year 2025 --extract time
[{"time":"2025-11-04T13:17:00Z"}]

$ jz run --assume-year now who
$ jz run --assume-zone JST=+0900 --assume-zone CET=+0100 date
```

- `--assume-year` takes a four-digit year or `now`. `now` is resolved
  once, when the command line is read, and dates each timestamp in the
  latest year that does not put it after that moment, with a day of
  allowance: `Dec 31` read on January 2 is last year, and `Feb 29` goes
  back to the last year that had one.
- `--assume-zone` takes `ABBR=+HHMM` and may be repeated.
- An assumption no field in the chosen format asks for changes nothing
  and is not an error.

## Seeing what a definition extracted

`--raw` skips the field rules, so each value is the text it was cut from.

```console
$ df -h | jz --extract use_percent
[{"use_percent":92}]

$ df -h | jz --raw --extract use_percent
[{"use_percent":"92%"}]
```

Nothing is converted, and `trim_prefix`, `null_if`, `required` and
`when_missing` are not applied. Every value is a string, except an empty
aligned cell and a regex group that did not take part in the match, which
stay null. It works with `--pretty`, `--stream`, `--extract` and
`--exclude`, and `jz test` does not take it.

## Seeing which parser was chosen

`--explain` writes on standard error which definition was chosen and why,
one fact per line, each line opening with `jz: explain: `.

```console
$ jz --explain --file df-gnu.txt
jz: explain: chose df/gnu from embedded
jz: explain: scope: every definition in the registry, by its signature alone
jz: explain: matched: signature.all[0] /\A(?:df: [^\n]*\n)*Filesystem[ \t]/
jz: explain: matched: signature.all[1] /^Filesystem\s+1K-blocks\s+Used\s+Available\s+Use%\.../
jz: explain: rejected: df/bsd: signature.all[1] /^Filesystem\s+512-blocks\s+Used\s+Available\s+Capa.../ did not match
...
jz: explain: not considered: <N> definitions only used when named
jz: explain: read: 8 lines: 7 read, 1 left out by input.ignore[1] /^Filesystem\s+1K-blocks\s+Used\s+Available\s+Use%\s+Mounted on\s*$/
```

The first line is the outcome: `chose`, `unidentified` when no definition
fits, `ambiguous` when several do and nothing settles it, `mismatch` when
a named variant does not fit, `defined` for `--define`, or `empty` when
the command `jz run` started printed nothing. Then:

| Line | What it says |
|------|--------------|
| `scope` | which definitions were candidates, and what named them: nothing (the whole registry), `--parser`, the name of the command `jz run` started, or the path of `--file` |
| `candidates` | when the command printed nothing, the variants its system and arguments leave; the answer is `[]` when each of them reads a list |
| `matched` | each condition the chosen definition states and the input met |
| `settled by` / `outranked` | when several definitions fit, the rule that chose one (the registry layering, or `detect.priority` between variants of one command) and each one it chose over |
| `rejected` | a definition that came close and why it was ruled out |
| `held back` | a definition whose signature fits but which is only used when named |
| `not considered` | how many definitions are only used when named, and so took no part (`<N>` in the examples here, since it follows the registry) |
| `read` | where the lines of the input went: read, joined by `fold`, blank, or left out by each `input.ignore` expression |
| `command` | for `jz run`, the command and the status it gave |

`jz run` also says what the system and the arguments narrowed:

```console
$ jz run --explain df -h
jz: explain: chose df/gnu-human from embedded
jz: explain: scope: the variants of df, from the name of the command jz ran
jz: explain: scope: narrowed by the system it ran on (linux) and its arguments (-h)
...
jz: explain: command: df -h (exit 0)
```

A search over the whole registry lists as `rejected` only definitions that
got past the first expression of their signature; one scoped to a parser
lists every variant it left out. When nothing is identified, the ordinary
message comes first.

`--explain=json` writes the same facts as one JSON document on a single
line that opens the same way:

```console
$ jz --explain=json --file df-gnu.txt 2>&1 >/dev/null | sed -n 's/^jz: explain: //p' | jq -c '{outcome, chosen: .chosen.definition, read: .read.read}'
{"outcome":"chosen","chosen":"df/gnu","read":8}
```

Every key is there whatever the outcome, `null` or empty where it does
not apply: `outcome`, `scope` (`from`, `parser`, `variant`, `os`, `args`,
`path`, `path_dropped`), `chosen` (`definition`, `registry`, `matched`,
`settled_by`, `outranked`), `candidates` (the definitions an ambiguous
input fits, or the ones an empty output was judged against), `rejected`,
`held_back`, `not_considered`, `read` (`lines`, `read`, `folded`,
`blank`, `ignored`, and `values`, the values a data file held), `command`
and `error` (`message`, `exit`). For a data file `outcome` is `format`,
`chosen.definition` names the format and `chosen.registry` what named it
(`--format` or `extension`).

With `--stream` the explanation is written before the first record and
has no `read` counts; records left out later get [lines of their
own](#watching-a-stream-for-the-records-it-left-out). `--explain`
changes neither standard output nor the exit status, and writes no clock
reading, so two runs over the same input explain themselves identically.

## What a file path says

A file has no argv, so its path is the only evidence besides the text:
the directory names the parser and the file name the variant.

```console
$ jz --file /etc/fstab       # etc/fstab
$ jz --file /proc/meminfo    # proc/meminfo
```

- The pair has to exist; `/etc` works because a parser named `etc` does.
  This reaches `auto_detect: false` formats such as fstab without naming.
- The definition still has to fit and read the text. If not, the path is
  dropped and the text is detected on its own.
- A name with a dot in it (`fstab.txt`) is ignored, and a path never
  names a shape definition.

## Naming a parser

Name a parser when detection cannot settle the input: a format too
generic to claim, a wrapper whose name is not the tool it runs, or a
variant you want pinned in CI.

```console
$ du -a /etc/cron.d | jz --parser du
$ df -h | jz --parser df --variant gnu-human
```

`--variant` needs `--parser`. Naming either is a claim about the input,
not a way around the checks: the definition's signature still has to fit
the text, and no option turns that off.

A variant with no signature, under a command whose other variants have
one, is reached only by naming the variant or by `jz run`. `ls/names`
reads `ls -1`, one name per line, which any list of lines fits:

```console
$ ls -1 | jz --parser ls --variant names
[{"name":"a b.txt"},{"name":"notes"}]
$ jz run ls /tmp/empty
[]
$ ls -1 | jz --parser ls
jz: no ls variant matches this input
...
No variant that checks the text fits it. If it is ls/names (...), which takes any text, name that variant:
...
```

`jz run ls` reaches it because it sees the arguments and rules out options
that add to the name (`ls -s`, `ls -i`). `ls --zero` (GNU coreutils 9.1
and later) keeps a name with a line break whole and is read by
`ls/names-zero`.

### Wrappers

`jz run` takes the parser from the name of the command it starts, so
`sudo`, `env`, `nice`, `timeout`, `stdbuf` and `busybox` put their own
name there. Name the parser and jz reads what the wrapper ran:

```console
$ jz run --parser df -- sudo df -h
$ jz run --parser ps -- busybox ps
```

jz keeps no list of wrappers. When it refuses a command whose arguments
name a program it has a parser for, it prints the command line that names
that parser; directories, files and option values such as `csv` are not
offered. Otherwise it suggests `jz run --parser PARSER -- COMMAND`.

```console
$ jz run nice -n 5 df -h
jz: no parser for "nice"
run `jz list` to see the supported parsers
If nice runs df, name that parser: jz run --parser df -- nice -n 5 df -h
```

### Files under /etc and /proc

`/etc/fstab`, `/etc/passwd` and their neighbours are variants of `etc`,
named by option or by [path](#what-a-file-path-says). `etc` without a
variant is enough where the file recognizes itself (`cat
/etc/nsswitch.conf | jz --parser etc`); `jz list etc` prints the variants.

Kernel files are variants of `proc`, and some are identified from their
text alone:

```console
$ cat /proc/meminfo | jz
[{"name":"MemTotal","value":64413356,"unit":"kB"}, ...,
 {"name":"HugePages_Total","value":0,"unit":null}, ...]
```

- `/proc/meminfo` is a list of counters. `unit` is `null` on the
  `HugePages_` counters, and numbers stay in the unit the file states.
- `/proc/cpuinfo` is read for x86 only; arm64 gets exit 4. Labels vary by
  vendor, so each block keeps whatever labels it holds.
- `/proc/diskstats` needs the twenty-field row of Linux 5.5 and later.
- `/proc/uptime` fits any pair of numbers, so it has to be named:
  `jz --parser proc --variant uptime < /proc/uptime`.

## Where jz stops and the command begins

Everything from the command name onwards belongs to the command, so its
own options never reach jz. A bare `--` states the boundary explicitly:

```console
$ jz run mytool --pretty            # --pretty goes to mytool
$ jz run --pretty -- mytool --json  # --pretty is jz's, --json is mytool's
```

## Checking definitions of your own

`jz test` holds a registry to the contract the official one is held to.

```console
jz test                      # the registries jz would use, except the built-in one
jz test ./registry           # a directory, layered above the built-in registry
jz test --update ./registry  # write testdata/<case>.json from the current output
jz test --decoys ./decoys .  # also require every file under ./decoys to be refused
```

- Every `testdata/<case>.txt` is parsed with its own definition and
  compared with the `.json` beside it, also with CRLF line endings, a
  byte order mark, or a blank line before or after it.
- Every fixture is changed (a foreign line added, the text doubled) and
  must be refused or show the change.
- Every definition is named on every other definition's fixtures and
  must refuse them. The official fixtures are inside the binary, so a
  signature wide enough to read `df` output fails here.

`--update` writes only to the registries under test; with no directory
it writes to the user registry and says where. Failures go to standard
error, followed by `N passed, M failed`. The exit status is 0 when
everything passed, 1 when something failed, 2 for a usage error and 5
when a registry could not be read.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | unexpected failure (I/O, internal) |
| 2 | usage error |
| 3 | the input did not match the chosen definition, held text the definition did not read, or yields more than 4,194,304 values, which is more than one document holds |
| 4 | the format could not be identified, several matched, or a named one did not fit |
| 5 | a registry could not be loaded |
| 141 | standard output was closed early, as by a pipe into `head`; nothing is said, and a command `jz run` started is stopped |
| *n* | `jz run` mirrors the command's own non-zero status, or 128+signal when it was killed |

Diagnostics go to standard error with a `jz:` prefix. Without `--stream`
a parse error leaves standard output empty. An output error, such as a
full disk, can interrupt a write in either mode.

## Registries

Definitions come from layered registries, and the first one that defines
a command and variant wins:

1. directories in `JSONIZE_REGISTRY_PATH`, separated the way `PATH` is
2. the user registry: `$XDG_CONFIG_HOME/jsonize/registry` on Linux,
   `~/Library/Application Support/jsonize/registry` on macOS,
   `%AppData%\jsonize\registry` on Windows
3. the registry built into the binary

`jz list --sources` prints them. jz never uses the network.

A registry's optional `registry.yaml` can switch off definitions of the
registries below it:

```yaml
format: 1
name: mine
disable:
  - file/posix   # one definition
  - du           # every variant of a command
```

A disabled definition is not loaded, so `jz list`, `--parser` and
detection stop seeing it. An entry that names nothing is a warning.

A registry file that does not parse, is too large, sits at a path that
disagrees with its command and variant, or cannot be read is named on
standard error and skipped, as is an unreadable directory. A registry
whose directory is missing or unreadable, whose `registry.yaml` cannot be
read or names another format, or whose `disable` list is not names, is
refused with exit 5.

When automatic detection is left with definitions of different commands
that all fit, the one from the earlier registry wins, so a definition of
your own can take over a format jz already reads. Two definitions of the
same registry that both fit stay an error naming them; `jz test` reports
those. The reasoning behind these rules is in [Design](../design/).
