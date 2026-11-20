---
title: Design
description: The decisions behind jsonize, the alternatives considered and what was left out.
toc: true
---

The decisions behind jsonize, the alternatives that were considered and what the MVP leaves out.

## Goals

- Make JSON for the next command from what a command printed, a data or
  text file, or command-line arguments, and stop there: selecting and
  reshaping JSON is jq's job.
- Read a command's output without writing Go per command.
- Make the chosen parser and the reason for choosing it visible; never
  produce plausible-looking JSON from the wrong parser.
- Keep definitions safe to accept from third parties.
- Behave identically in a terminal, a pipeline and CI.

## Architecture

```
cmd/jz                    entry point: signals, exit code
internal/cli              subcommands, flag parsing, registry layering, exit-code contract
internal/runner           exec mode: child process, LC_ALL=C, stderr passthrough, output cap, signals
internal/conformance      golden cases, tampered fixtures and the definition/fixture cross product, shared by `go test` and `jz test`
internal/schema           the JSON Schema of a definition's output, its validation and the compatibility rules
internal/datafile         data and text formats read by extension or --format (JSON, JSON Lines, LTSV, YAML, text, lines, nul)
internal/jsonbuild        jz new: the arguments planned into a document, locations checked before anything is read
internal/yaml             the YAML reader behind definitions and YAML data files
internal/buildinfo        the version string stamped at build time
pkg/registry              load registry directories/FS, merge with precedence, testdata cases
pkg/definition            YAML schema, validation, regex compilation, format checks
pkg/selector              variant selection (os / args / signature filter → precedence → priority)
pkg/engine                parse algorithms (table, csv, ini, tree, regex, kv, composite, records), streaming, field conversion
pkg/convert               scalar conversions (int, float, bool, time, duration)
pkg/jsonutil              insertion-ordered JSON object and encoder
registry/                 the official definitions and fixtures (data) + a one-file embed
e2e/atago                 end-to-end scenarios (e2e/README.md says what they guarantee where)
```

Data flows one way: capture, select, parse, encode. The selector needs only
the first lines of the text, the engine needs the compiled definition and
the whole text, and encoding never sees definitions.

## Decisions and trade-offs

### A small public surface

The command line has six subcommands (`run`, `new`, `list`, `test`, `completion`,
`version`)
and the options listed in the usage page. Options that asked the user to
make a decision jz should make, or that changed the output contract, were
removed before release:

| Removed | Why |
|---------|-----|
| `--meta` | an envelope that changed the JSON schema depending on a flag |
| `--os` | asking the user to declare the producing system instead of matching the text; jz knows the system when it runs the command itself |
| `--force` | a way to parse with a definition whose signature says the text is something else, which is exactly the failure jz exists to prevent |
| `--max-input` | a safety limit, not a preference: 64 MiB of input and 4,194,304 retained values, internal |
| `--embedded-only`, `--registry` | registry plumbing; `JSONIZE_REGISTRY_PATH` covers the real use and tests inject sources directly |

### Definitions in YAML rather than Go plugins or a scripting language

Definitions use a fixed set of parsing rules. They can split fields, match
expressions and build nested records, including trees whose depth comes
from the input, but they cannot execute code or evaluate expressions, so
each one can be reviewed and tested against captured output. jz reads the
YAML with its own reader, which covers the part of YAML definitions use and
names the line of every problem, including a key this build does not know.

### Eight parse types instead of a pipeline

`table`, `csv`, `ini`, `tree`, `regex`, `kv`, `composite` and `records`
cover the shapes the registered commands print. A composable step pipeline
(split, map, filter) was rejected because it is harder to validate and
explain, and every real example so far fits one of the eight. `input.select`
(after/until/skip/limit) with `composite` handles sections, `records`
applies one description to a repeating block, and a `composite` part may
be `records` or `tree` for a report with a header above the repeats. Adding
a type does not change the format version, because an unknown type is
already an error.

Most of them assume one line is one record. `csv`, `ini` and `table` with
`split: box` each break that: a CSV value may contain a line break, an ini
file is sections, and a drawn table marks its rows with rules. A new type
would have to break it in a way none of these does.

### Data files are read by extension, not detected

A CSV, TSV, LTSV, JSON Lines, JSON or YAML file is read as the format its
extension or `--format` names, by a reader of that format, and nothing is
guessed from the text. Text with no format of its own is read as strings
with `--format text`, `lines` or `nul`, which have no extension, because a
`.txt` file is more often a command's output than a list of lines.

A CSV value stays text unless `--type` names its column. Data file
converters usually go wrong by guessing that `007` is a number or `1` is
true. `--type` uses the same conversions as a definition's fields, so there
is one set of rules. XML, TOML, plist, x509 and JWT stay out: each has a
specification that makes reading it a project of its own, and a tool whose
whole job is that format.

### Table splitting

- `whitespace`: split on runs of whitespace with the last column
  absorbing the remainder. Used for `df`, `ps`, `free`.
- `aligned`: cut cells at the rune offsets where header words start,
  moving a boundary left when a right-aligned value is wider than its
  header and keeping a token whole when it overflows to the right. Empty
  cells become `null`. Needed for `lsblk` and `w`.
- `delimiter`: a literal separator (`du`'s tab).

The official registry prefers explicit `header.columns`, because derived
names depend on the exact header text. Derivation exists for quick local
definitions.

### Variant = output format

There is one definition per output format, not per implementation. GNU and
BusyBox `df` without options print the same table, so one definition covers
both and lists them in `metadata.compatible`. `df -h` differs between them
("Avail" vs "Available") and gets two definitions. A definition claims
exactly what its fixtures prove.

### Rounded numbers stay text

`df -h` steps by 1024 and `df -H` by 1000; the output records neither, and
both round. Deriving bytes from "1.1G" would invent a base and a precision,
so human-readable sizes are reported as printed, and the exact forms of the
same commands (`df`, `free`, `ls -l`, `lsblk -b`) produce numbers. For the
same reason there is no `ls -l` / `ls -lh` split: the text does not say
which one produced a listing, and with the size kept as printed it does not
need to.

### Trees: the input sets the depth, the definition sets the shape

`lspci -vv` prints a device, its capabilities and each capability's flags.
`lsusb -v` prints a device, configuration, interface, endpoint and endpoint
attributes. How deep that goes depends on the machine, so no definition can
state it. `type: tree` splits the question: the definition states the
fields read from a node's line and that children go in a `children` array,
and the input decides only how deep the nodes go.

Every line is one node, read by `node.parse` (an ordered list of regular
expressions, or a key/value split), and the same description applies at
every depth. Grouping runs of lines into one node was rejected because a
node's identity would then depend on its siblings.

Three limits keep the input from deciding more than the depth. A line
indented two levels below the one above it has no parent and is an error.
Indentation that is not a whole number of the stated unit is an error, not
rounded down. Depth stops at 32, so a producer cannot make jz build an
unbounded stack of objects.

`indent` states one level as written, and accepts a list of forms one level
may take. tree(1) writes a level as one of `"|   "`, `"    "`, `"|-- "` and
``"`-- "``, `systemd-analyze critical-chain` as two spaces or a backtick
and a dash, and `wpctl` as four spaces or a branch character. Each is a
fixed width whose characters depend on whether anything follows, so a list
of alternatives counts them where a single string cannot. A `level`
expression capturing the leading whitespace was dropped, because it still
leaves open how much of the capture is one level.

`tree` may be a `composite` part (a banner above the tree) or a `records`
part, which `sensors -u` needs for a block per chip with a two-level tree
of features inside.

### A definition can be given instead of named

Output with no definition exits 4, but the caller often knows the shape and
only wants this reading once, without a registry. `--define` takes the
definition itself: a `parser.yaml` without the four keys that place it in a
registry, leaving `parse` and optionally `input` and `fields`. Nothing is
detected, so it cannot be combined with `--parser`. The body goes through
the same loader and validation as a file, so an inline definition can say
nothing a file cannot, and one worth keeping moves into a registry
unchanged.

The registry also has definitions that describe a shape rather than a
command: `table/whitespace`, `table/aligned`, `table/box`, `csv/comma`,
`csv/tab`, `kv/colon`, `kv/equals` and `ini/default`. They are `--define`
under a name.

A definition with no signature and `auto_detect: false` makes no claim
about the text, so the check that asks whether it reads a neighbour's
output would flag every fixture in the registry against `table/whitespace`.
`selector.ShapeOnly` names that class. The conformance runner leaves those
out of the cross product and requires the variant to be named, since
`table/whitespace` and `table/aligned` read the same lines two ways and no
text can separate them.

### Three formats that are not line-oriented

A CSV value may contain the delimiter, a quote or a line break, so the
lines are joined back together and read with `encoding/csv`. A streamed CSV
holds a line back while a quote that opened a value is still open; counting
quotes instead mistook a quote in the middle of a value for an open one and
held every line after it. An ini file is sections of keys, so it is one
object with no streaming form. A drawn table (`split: box`) takes its cells
from the bars, and a value wrapped over three lines is one cell.

MySQL, psql, `sqlite3` in box mode and `duf` draw rules only around the
table and under the header, so every body line is a row, and a wrapped
value's continuation has an empty first cell. A row whose first column is
blank therefore reads as a continuation.

The three needed no new key beyond `split: box`. Their fuzz targets found
that `StripANSI` in the whole-document reader took a line break as the end
of an escape sequence (`"\x1b\n"`) and joined two records; no escape
sequence may consume a line break now.

### A repeating report is an array of samples

`iostat 1 3` prints its column header once per sample. The second header
used to reach the device table as a row and fail to convert, so the whole
report exited 3. `mpstat` and `pidstat` had the same fault. These reports
are now an array of samples: `type: records` with the sample's header line
as `start`, and the tables inside a sample as its parts. A single run is
the same shape with one sample. That broke what those definitions returned,
and it makes `--stream` useful: one sample per line, written as soon as it
is complete. No new key was needed, because inside `records` a part's table
already takes its header from the first line of its region.

The sysstat banner (kernel, host, CPU count) is dropped. `uname -a` and
`nproc` print the same facts, and keeping it would mean an object for the
banner and an array for the rest, which has no streaming form. `vmstat` and
`sar` were left alone: they print their header once with every sample
under it, so an interval run was already one table with a row per sample.

### A duration is seconds

`ps` prints `00:04:14`, `uptime` prints `13 days, 4:22` and `w` prints
`13:42m`. A consumer has to subtract, compare or sum these, so
`type: duration` reads them and reports seconds.

A bare two-part reading is ambiguous: `4:50` from `ps` is four minutes
fifty seconds, `1:23` from `uptime` is an hour and twenty-three minutes. So
`layout` is required (`h:mm` or `mm:ss`) with no default, because a wrong
guess is a factor of sixty nobody downstream would notice. A trailing unit
letter settles it where the format prints one, which is how `w` separates
`13:42m` from `13:42`.

The rounded-numbers rule is about what the text names, not how coarse the
value is. `1.8T` names no particular number of bytes until a base and a
precision are chosen. `13 days, 4:22` names exactly 1138920 seconds; the
machine has been up a little longer, but the stated length is not
ambiguous.

What the rule does exclude is a column whose shape changes with the value.
`top`'s TIME+ prints `mmm:ss.hh` and falls back to `hhh,mm` for a
long-running task, so one field would carry seconds in one row and minutes
in another. It stays text. `duration` does not read the comma form, because
allowing a comma would make every other value containing one ambiguous.

### Assumptions are stated on the command line, never inferred

`who` prints `Nov  4 13:17` with no year, and `date` prints `JST`, which is
+0900 only with a table of abbreviations; the same letters name different
offsets in different places. The caller often knows which year a log covers
and what zone the machine was in, so jz asks: `--assume-year 2025` (or
`now`) dates a field whose definition says `year: assumed`, and
`--assume-zone JST=+0900`, repeatable, gives an abbreviation an offset.
Without them the value stays the printed string. `year: assumed` alone
converts nothing; it is a statement about the format, which is why it lives
in the definition.

`now` is resolved once when the command line is read, and no zone database
is consulted: Go resolves an abbreviation against the running machine's
zone, so the same text would convert differently on two machines.

An assumption no field asks for is not an error, so one command line can
serve several formats without knowing which print a bare year. A key named
to `--extract` is different: it changes the shape a consumer reads, so it
has to be right.

### Some formats are only used when named

A weak signature can still be met by unrelated text: a number, a tab and a
path is `du` output and `git diff --numstat` alike. Such a definition sets
`detect.auto_detect: false`, which keeps it out of automatic detection
while `--parser du` and `jz run du` still reach it, signature check
included. Adding exclusion patterns for every lookalike format would be a
list that never ends.

A definition with no signature at all goes further. `ls -1` is one name per
line, which any list of lines is, so `ls/names` makes no claim about the
text. `--parser ls` on a pipe cannot tell `ls -1` from `ls -s` output that
the long variants refuse, and the signature-less variant would then read a
size column as part of each name and exit 0. So that variant is reached
only by naming it (`--variant names`), or by `jz run`, where the arguments
have already ruled out options that put more than a name on a line.

### Arguments say where a value goes and what it is made from

`jz new` has one grammar for what a value is (`=` text, `:=` JSON, `=@` a
file's text, `:=@` a data file) and two for where it goes: a key, or a JSON
Pointer with `--path`. A dotted key was rejected because
`app.kubernetes.io/name` is an ordinary key. RFC 6901 already escapes the
two characters a pointer needs (`~1`, `~0`).

```console
$ jz new --path /a/b=1 --path /a/c:=true
{"a":{"b":"1","c":true}}
```

A pointer is a location, not an update. It creates containers on the way
and adds members and elements. A location given twice, a pointer into a
value given whole, and an index past the end are refused, because each
would make jz pick a winner, change a type or invent elements. Editing a
document is jq's job.

`--string` and `--text-file` exist because a plain argument reads its
value: `key=$VAR` becomes a file name when the variable starts with `@`.
Each takes KEY=VALUE as one argument, so a value that looks like an option
cannot be taken for one.

### A record at a time

`jz new --each` reads standard input as records rather than one value, so
a running stream can be wrapped with fixed fields (a host, a source) around
each record. A record is placed whole; filtering records or computing
fields needs a language jz does not have.

Every stream treats a record it cannot read the same way, whether it comes
from a command, a data file or plain lines: the record ends the stream, the
records before it stand, and the status is 3. See [a stream ends at the
record it cannot read](#a-stream-ends-at-the-record-it-cannot-read).

### JSON is the only output

jz makes JSON for the next step of a pipeline, so the output has one form
and one contract, the schemas under `registry/schemas`. An earlier `--yaml`
needed mostly a quoting rule, because YAML 1.1 and 1.2 readers type `yes`,
`1:20` and `2026-09-11` differently. It was removed: a second format
doubled what every output option had to be tested against, and a YAML tool
reading jz's JSON gets the same values. `--yaml` is now refused with a
message that says so.

### Signatures say what a format is

A signature is a claim about the text, written as what must be true. `none`
is not how a definition earns its answer.

`file/posix` reads "path: description", the shape of a dozen other
reports, and once collected an exclusion for each one it met: `blkid`,
`lscpu`, `git stash list`, `udevadm`, `modinfo`, `nmcli`, `pip show`, a
Debian control record, `fc-list`, `ethtool -i`, fourteen in all. Each entry
made `file/posix` a definition other people had to edit before theirs
would pass. The replacement says two things about file(1) output: every
line is "path: description", which is where `apt show`, `dpkg -s`,
`zipinfo` and `lscpu` stop resembling it, and some description names a
kind of file from a list this definition owns. Fourteen exclusions became
zero.

Signatures are reviewed against four rules:

- Write the whole shape. Anchoring both ends separates `uptime` from `w`,
  which opens with the same line, and `ipcs -q` from `ipcs`, which opens
  with the same section. `\A` and `\z` do most of the work a `none` used to.
- A signature may narrow what the definition undertakes to read.
  `git log --oneline` is read for an abbreviated hash of seven to twenty
  digits, which leaves no room for a 32, 40 or 64 digit digest; the price
  is `--no-abbrev`, and the definition says so.
- Unknown is an answer. A format jz cannot claim exits 4. The failure to
  design against is text read with the wrong definition and returned with
  status 0.
- `none` is for when there is no positive form to write, and the reason
  goes beside it. One remains, in `etc/hosts`: `ip neighbour` prints an
  address, "dev", an interface and a state, and every word of that is a
  legal host name.

Adding a definition should need only its own signature and fixtures;
editing someone else's means the two were coupled through an exclusion. To
measure it, take a `none` out and run `jz test`. Of the 68 the registry
had, 31 excluded nothing, 13 named another command, and 11 remain, each
telling variants of a single command apart.

### Fixtures verify a signature, they do not decide it

Decide what the format is, write the signature, then capture fixtures that
show it. "No fixture misdetected, so the signature is fine" holds only
until someone adds a fixture. `file/posix` has one fixture line per kind
the signature names, a listing that mixes a kind with lines file(1) could
not read, and a listing that names no kind and is refused. Each exists
because the signature says something.

### `--raw` shows the text before conversion

`--raw` was first removed as a second output shape for the same input. It
came back for debugging: when a value is wrong, the definition either cut
it from the wrong place or converted it wrongly, and after typing the two
look alike. `--raw` shows the text each value was made from.

```console
$ df -h / | jz
[{"filesystem":"/dev/nvme1n1p2","size":"1.8T","used":"1.3T","available":"426G","use_percent":76,"mounted_on":"/"}]
$ df -h / | jz --raw
[{"filesystem":"/dev/nvme1n1p2","size":"1.8T","used":"1.3T","available":"426G","use_percent":"76%","mounted_on":"/"}]
```

It skips the field rules and nothing else: no conversion, no
`trim_prefix`, no `null_if`, and no `required` or `when_missing`, since a
shape that changed with the values would defeat the point. Every value is a
string, or null for an empty aligned cell and a regex group that did not
take part. `jz test` does not offer it, because it compares fixtures
against the typed JSON.

### The choice is visible

`--explain` writes to standard error which definition was chosen and why,
and changes nothing about the answer, so it can be added to a working
pipeline. It writes no timestamp, so two identical answers compare equal.

```console
$ uptime | jz --explain >/dev/null
jz: explain: chose uptime/linux from embedded
jz: explain: scope: every definition in the registry, by its signature alone
jz: explain: matched: signature.all[0] /\A\s*\d{1,2}:\d{2}(?::\d{2})?\s+up\s+.*,\s+load av.../
jz: explain: rejected: systemd-analyze/blame: signature.all[1] /^[ \t]*[0-9][^\n]*[ \t]\S+\.(?:service|socket|targ.../ did not match
jz: explain: not considered: 83 definitions only used when named
jz: explain: read: 1 line: 1 read
```

Rejections are filtered. A search of the whole registry rejects three
hundred definitions on the first signature expression, so only near misses
are listed: a definition past its first signature expression, or one whose
signature fits but `auto_detect: false` held it back. A search scoped to
one parser lists every variant.

The other lines give the scope of candidates (the whole registry, a named
parser, the command `jz run` started, a file's path), the rule that
settled a tie, how many definitions are only used when named, and where
the input's lines went. The first line names the outcome: `chose`,
`unidentified`, `ambiguous`, `mismatch` or `defined`; no match and several
matches both exit 4 but need different fixes. There is no confidence
score: every line names a rule that can be checked against `jz list`.

`--explain=json` gives the same facts as one line opening `jz: explain: `,
on standard error, because standard output carries only the conversion.
The prefix is kept because `jz run` passes the command's own standard error
through on the same stream. Every key is present whatever the outcome.

### A file path is evidence

`jz run` narrows variants with the command name and arguments, and a pipe
has only the text. A file has no argv, but its path says something the text
does not. The rule: the directory names the parser, the file names the
variant, and the pair has to exist in the registry. `/etc/fstab` and
`/proc/meminfo` work because parsers named `etc` and `proc` exist; nothing
in Go knows either path. This helps exactly the formats a signature cannot
claim: `/etc/fstab` is six whitespace-separated fields, so it declares
`auto_detect: false` and otherwise has to be named every time.

```console
$ jz --explain --file /etc/fstab >/dev/null
jz: explain: chose etc/fstab from embedded
jz: explain: scope: etc/fstab, from the file path /etc/fstab
jz: explain: matched: signature.all[0] /\A(?:[ \t]*(?:#[^\n]*)?\n)*(?:/\S+|UUID=\S+|LABEL=.../
jz: explain: read: 14 lines: 4 read, 1 blank, 9 left out by input.ignore[0] /^\s*#/
```

The path only adds an answer. The definition it names still has to satisfy
its signature and read the text; failing either drops the pair and the text
is read on its own terms. A base name containing a dot is a file name, not
a variant, which keeps `fstab.txt` out without a list of extensions.

Shape definitions are excluded. They were not at first, and `df` output in
a file called `comma` inside a directory called `csv` was read as CSV at
exit 0: a shape has no signature, so the directory name was the whole of
the evidence. Reading the base name as the parser (`df.txt` → `df`) was
rejected too, because a file called `ls` in a directory called `bin` would
name a real parser with no variant to confirm it.

### Selection never guesses

Candidates are filtered by criteria that apply; one whose input is unknown,
such as arguments in pipe mode, neither helps nor hurts. One survivor is
the answer, and there is no ranking by closeness of fit. Several survivors
are settled by `detect.priority` only when they are variants of the same
command and one priority is strictly highest; anything else is an error
that names the candidates. "First match wins" would depend on directory
order.

Survivors from different registries are settled first by layering: the
earlier registry wins. Otherwise a `^Filesystem` signature in a user
registry made `df -h | jz` report several matches and exit 4. Inside one
registry definitions are never ranked, so a collision stays an error that
`jz test` reports.

Fixtures double as selection tests: each must select its own definition,
and every other definition named explicitly on it must refuse it, so a
variant whose signature overlaps an existing one fails `jz test`.

### Exec mode forces `LC_ALL=C`

Output formats are documented for the C locale; translated headers and
localized numbers break parsers. jz sets `LC_ALL=C` and `LANG=C` (and
drops `LANGUAGE`) unless `--keep-locale` is given. Pipe mode cannot
control the producer, which is why signatures match structure rather than
prose where possible.

### Exit status of `jz run`

A failing command's status is mirrored, and its output is still parsed
when there is any (`df` exits 1 when a mount point is unreadable but prints
the table). jz's own codes (2 to 5) are distinct from 0 and 1, but they can
collide with a child's codes; the stderr line `jz: <cmd> exited with status
N` tells them apart.

### Streaming is an option, not the default

A command that does not end (`vmstat 1`, `iostat 5`) has no whole document
to write, so `--stream` writes one JSON document per line as each record is
read. It is the only option that changes the output contract: elsewhere
standard output carries a complete document or nothing, and here every line
is a complete document. Making it the default would give up, for everyone,
the guarantee that a failure leaves nothing behind.

### A stream ends at the record it cannot read

Reading a whole document, one record that does not fit means the text is
not the claimed format: nothing is written and the status is 3. A stream
has already written earlier records, so the record that cannot be read ends
it, the earlier records stand, the failure is named on standard error with
its line number, and the status is 3.

```console
$ printf '{"a":1}\nnot json\n{"a":2}\n' > s.jsonl
$ jz --stream --file s.jsonl
{"a":1}
jz: jsonl: line 2: the line is not one JSON value
$ echo $?
3
```

Streams once skipped the bad record and went on. But a stream with a gap
looks like one without to the next program, which does not read standard
error and acts on the records before the exit status arrives. So the
stream stops, and `--stop-on-error` was removed (naming it is refused with a
message).

The other failures end a stream the same way. Text whose format cannot be
identified is settled on the leading lines before anything is written
(exit 4), a format that reads its whole output into one object has no
streaming form (exit 2), and a line over the limit, a read error or a
missing `--type` column leave no next record to find.

`engine.Stream` takes only an `emit`, and the conformance runner checks
that a fixture read whole and read as a stream agree record for record.
When `jz run --stream` ends at a record and the command also fails, the
command's status wins; the failure is on standard error either way.

### Every line is accounted for

The goal of never producing JSON from the wrong parser has a second half:
JSON from the right parser that is missing something. Two `dig` replies in
one capture once read as the first reply at exit 0, because the second
fell between the parts. `pactl list sinks` lost every property, port and
format of every sink, because the attribute part named those lines as
belonging to a sibling that did not exist. Golden files could not show
either, since a skipped line leaves no trace in them.

So the engine keeps a ledger. A line is read when a parser turned it into
part of the result, left out when a rule the definition states says so
(`ignore`, a blank line, a `fold` continuation), or unread. Unread text is
exit 3 with the line quoted. The rule lives in the engine, so no definition
can opt out. When it arrived, 37 definitions failed on their own fixtures,
most for a heading they had never stated in full, and the checks below
found two more that only real output exposed. The `dig` capture became exit
3, and the definition now reads one reply per query.

The finer rules:

- A pattern has to reach both ends of the line it reads; otherwise a
  trailing field nobody asked about would pass. Two lines in the whole
  registry broke this.
- A heading `select.after` matches counts as read only when the expression
  states the whole line. `'^Features for '` finds a position; it does not
  say the interface name after it is worthless.
- A part's `ignore` hands a line to a sibling; it does not drop it. That is
  how `pactl` lost its properties.
- A key printed twice into an object is an error, whatever the two values,
  because two copies of a value are what two documents read as one look
  like.
- `ignore` is the one way to leave text out on purpose, and it is checked
  from outside. `jz test` adds a foreign line to every fixture, at the end
  and in the middle, and reads every fixture twice over; a definition that
  succeeds must carry the line in its result, and a list read twice must be
  the records of one copy twice. That found `upower`'s catch-all
  `ignore: ['^[^:]*$']`, which dropped a battery's device type and history
  rows, and a table's second header read as a row of column names.

Blank lines are never unread. They carry no value, and a definition that
keeps them (`skip_blank: false`) should not have to name the ones it does
not want.

The ledger guarantees only that a successful conversion did not skip input
without saying so, not that the reading is right. A table with explicit
`header.columns` takes its first line as the header without checking its
words (the signature does, inside its window), and a pattern spanning lines
with `[^\n]*` or `(?s).*` reads what it spans without reporting it.

The sysstat banner, `xrandr`'s Screen line and the count under `tree` are
still dropped on purpose, each matched as a whole line. The `dig` question,
EDNS options and warnings, `mtr`'s host, `pactl`'s properties, ports and
formats, and `upower`'s device type and history are now read.

### Registry layering and the code/data boundary

`registry/` holds only YAML, fixtures and one `embed.go`. `internal/*`
never imports it; only `cmd/jz` and the golden test do. Moving the
registry to its own repository means changing one import.

Layering (`JSONIZE_REGISTRY_PATH`, then the user registry, then embedded)
lets a user fix a parser locally and send it upstream later with no change
in behaviour.

A command that names its parser, `jz run COMMAND` or `--parser NAME`, reads only the built-in definitions that answer to the name: those in the name's directory and those that list it among their aliases. Reading all of them was most of what a short conversion cost, about 45 ms of a `jz --parser df` run on one core, against about 5 ms for the part the name needs. Detection reads every definition, since any of them could fit the text. When a registry of the user's own is present, or no definition answers to the name, every definition is read as before, so the names jz suggests and the warnings about a disable list stay what they were.

### No network, ever

jz reads local directories and nothing else. A release carries both the
code and the definitions, so an input converts to the same JSON on a given
machine whatever the network is doing. Updating the official registry
means installing a new jz; adding your own means pointing
`JSONIZE_REGISTRY_PATH` at a directory.

### The output schema is derived from the definition

A consumer needs to know which keys of which types will always be there.
The definitions already say it: a column is a key, a named group is a key,
`type: int` is an integer, `when_missing: omit` is a key that may be
absent. So the schema is generated from the definition; a hand-written one
would be a second statement of the same facts.

The generator derives only what the engine guarantees. A key is required
when every object of its kind carries it, a value is nullable where the
engine can leave it empty, a group matching only a few literals becomes an
`enum`, and a table with header-derived names uses `additionalProperties`. Every
fixture is validated against its schema by jz's own validator and by an
independent one in CI, and a disagreement fails the build.

The schemas are published in `registry/schemas` and on the site at the
`$id` each states, and each carries a contract version in
`x-jsonize.version`. That is separate from `format`, which versions the
definition language. They change for unrelated reasons: the `iostat`
rewrite changed an output from an object to an array without touching the
language.

A change is breaking when a program reading the output, or a document kept
from before and validated against the new schema, can break: a key
removed, a type changed (including made nullable, and an object become an
array), a key no longer always present, a key now always present, an
`enum` value added or removed, an element type changed. The one change
that breaks neither reader is a key that may now appear and never has to.

`make registry-update-schema` rewrites the files and stops at a break;
`BREAKING=command/variant` publishes it under the next version. CI compares
the schemas against the branch being merged into, so a schema edited by
hand to match a break, with the version left alone, is caught. Removing a
definition ends its contract, which is recorded by listing it in
`registry/schemas/retired`.

The version is not written into the JSON jz prints, so documents compare
equal across releases; `--explain` names the definition, which identifies
the contract.

### Format versioning

`format: 1` is the major version of the definition language, and a
different number is rejected with a message that says whether to upgrade
jz or the definition. It says nothing about what a definition produces;
that is the output contract's version, above.

Until the first tag, format 1 is not frozen: keys are added, renamed and
removed while the shape settles. After the tag, adding a key is a minor
release and the number stays 1. An older jz meeting a definition that uses
the new key refuses it with an unknown-key error saying a newer jz may be
needed, so a definition works with the build it was written for and every
later one. Removing a key or changing its meaning is `format: 2`, because
neither can be told from a mistake by looking at the file.

`min_jsonize`, a version a definition could demand, was removed: it asked
authors to know which release introduced each key and said nothing when
they got it wrong. The unknown-key error names the key at fault.

### Dependencies

None outside the Go standard library. The YAML reader (`internal/yaml`)
reads block and flow mappings and sequences, plain, quoted and block
scalars, comments and one document per file, and refuses anchors, aliases,
tags and keys that are not one scalar with a message that names them.
Before the YAML library was dropped, the reader was held to the same values
over every file in the registry. A golden failure shows the two texts.

No JSON Schema library: the generator emits a small subset of draft
2020-12, a validator for that subset is shorter than the dependency, and CI
checks the same fixtures with the Python `jsonschema` package. No CLI
framework: `flag` and a dispatch table serve six subcommands, and `flag`
already stops at the first non-flag as `run` needs.

### Commands that share a format

An `aliases` entry lets one definition read output from several command
names. `vdir` uses the `ls` definitions, `getent passwd` uses `etc/passwd`,
and `printenv` uses `env`. An alias can replace the argument filter when
its command line differs. See [aliases](../definition-format/#aliases).

## Out of scope for the MVP

- Computed fields that combine input fields, such as subtracting used
  space from total space. Converting one duration field to seconds is
  supported.
- Fetching or updating registries over the network.
- A JSON Schema for editor completion of `parser.yaml`; validation is
  done in Go with path-qualified messages instead.

### A table that counts its fields

A `table` with `split: whitespace` gives its last column whatever is left
on the line, which `ps aux` needs for a command with arguments. That also
meant a row with N+1 fields produced N cells with two values in the last.
`max_fields` may now be one more than the number of columns: the split
then yields a cell with no column name, and the row is refused as too
wide, on every row and whatever the last column's type. No key was added,
since `max_fields` already meant the most fields the split produces.

`proc/diskstats` needed this. Its count used to come from two places: the
signature counted fields inside its window, and past the window a
twenty-first field was refused only because it landed in `flush_ms` and
failed to convert to an integer. A text last column would have taken it
silently. It now sets `max_fields: 21`.

### The library under pkg/

The six packages that read text are under `pkg/`, so another program can
load a registry, select a definition and parse with it without the command
line. `internal/` keeps what only the binary needs: the subcommands, the
child process, the conformance runner and the version string. The
exit-code contract, flag parsing and process handling are decisions about
a command line, and a library caller has already made them. Fixtures and
the checks that run them live with the conformance runner under
`internal/`.

A `Definition` comes only from `Load`, which sets its unexported compiled
expressions, so one built by hand or changed after loading is not read as
written. The slice `Variants` returns belongs to the caller.

What `cmd/jz` and the tests do not reach is unexported. The enum
vocabularies of the definition schema (`RecordNUL`, `UnitBinary`,
`MissingNull` and their siblings) are all exported, because they name
values a caller reads out of a `Definition` and half a set would be worse.

## Open questions

Nothing here is implemented; each is listed because a definition in the
registry works around it today.

### What a shape definition does about types

`table/whitespace` and its siblings convert nothing, because a shape says
nothing about what its columns mean. Someone naming one usually does know,
and can say so only with a full `--define`. A `--field name=int` option
would close the gap, but the same thing is one line of `jq`, and it would
be a second place where types are decided.

### Where the sysstat banner went

Reading `iostat` as an array of samples dropped the banner (kernel, host,
architecture, CPU count). A composite could keep it as a separate part,
but that would change the published output schema, and putting it on every
sample would need another rule. `uname -a` and `nproc` supply the same
information. What would close it is a way for a definition to say that
some fields belong to every record of a stream, and inventing that for one
report would be premature.

### Discrete field counts in a table

A table states one number of columns, and `max_fields` states whether a
wider row is refused (see "A table that counts its fields"). Neither states
a set of widths. `/proc/diskstats` has fourteen, eighteen and twenty fields
across kernel versions, and fifteen, sixteen, seventeen or nineteen are
none of them. A range would accept rows no kernel wrote, and a list of
widths would be a key with one caller.

Field counts cannot settle everything anyway: a twenty-field row truncated
to eighteen looks like a row an older kernel wrote, and separate variants
would not change that.

### A column that prints two layouts

`ls -l` prints `Jan  5 10:11` for a file changed in the last six months
and `Apr  9  2025` for an older one, so the layout depends on the file's
age. A field takes one layout, so the column stays text; `year: assumed`
does not help, since this format prints a year for some rows. The options
are two more regex alternatives in a definition that already has two, or a
field that states a list of layouts tried in order. The second is probably
right, but it would be the first place a field guesses, and the open
question is whether trying layouts in a stated order counts as guessing.
`who`, `last` and `journalctl -o short` all print one layout, so nothing in
the registry needs it today.

### Whether a definition should split a value it can

A field of type `object` splits a value on a regular expression, so a
definition can turn `1024 KB` into a number and a unit, or
`48 bits physical, 48 bits virtual` into two numbers, as `proc/loadavg`
splits `4/4688` into a runnable count and a total. The registry is not
consistent about it: `proc/meminfo` separates every counter from its unit,
while `proc/cpuinfo-x86` reports `cache size`, `TLB size` and
`address sizes` as the kernel wrote them. Until there is a rule for which
values to take apart, changing either would trade one inconsistency for
another.

## Where to cut next

- `registry/` → separate repository; only the `official` import changes.
