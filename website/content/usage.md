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
jz run COMMAND [args...]     # let jz run the command and convert its stdout
```

The mode is always explicit, so a pipeline behaves the same in a shell
and in CI. Whether standard input is a terminal is consulted for one
thing only: `jz` with no arguments at all prints help instead of waiting
for input that is not coming. Piped and redirected input take the same
path either way.

`jz run` hands the command your standard input, passes its standard error
through untouched, mirrors its exit status, and runs it with `LC_ALL=C`
so the output is the one the parsers describe.

A command that succeeds without printing anything is answered with `[]`,
which is what a command that lists things prints when there is nothing to
list. That answer needs a format that has an empty form, so a parser
whose variants read a single object says it cannot tell instead. On a
pipe there is no such answer at all: nothing identifies an empty input.

## Options

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
      --explain[=json]          report the chosen definition and why, on stderr
  -h, --help                    show help
```

`jz run` adds `--env NAME=VALUE`, `--keep-locale` and `--timeout`, which
control the command rather than the conversion. `jz list` adds `--json`
and `--sources`, and `jz test` adds `--update` and `--decoys`.

## Choosing the keys

`--extract` keeps the keys it names and `--exclude` drops them. Both may
be repeated, both apply to every object jz prints, and they cannot be
combined: a key named on both sides would have two answers.

```console
$ df -h | jz --extract filesystem --extract mounted_on
[{"filesystem":"/dev/nvme0n1p2","mounted_on":"/"}, ...]

$ jz run --exclude 1k_blocks --exclude used df
```

A key that the format does not produce is an error naming the keys it
does have, rather than an empty object or a silently ignored request:

```console
$ df | jz --extract mountpoint
jz: no key "mountpoint" in the output
the keys it has are "1k_blocks", "available", "filesystem", "mounted_on", "use_percent", "used"
```

The keys named are the ones at the top of each object. A value nested
inside an object keeps whatever it holds.

## Reading a command that keeps printing

`ping`, `vmstat 1` and `tail -f` do not end, so there is no whole
document to write. `--stream` writes one JSON document per line, each one
as soon as the record behind it is complete:

```console
$ jz run --stream ping -c 100 1.1.1.1 | jq -c 'select(.time_ms > 20)'
$ vmstat 1 | jz --stream
```

Only a format that yields records can be streamed: a table, a regex
matched per line, a key/value list, and `records`. A format read into one
object (`composite`, `each: input`, a kv map) is refused with
`format <id> has no streaming form` and exit status 2, because there is
nothing to hand over until the last line has arrived. `--pretty` is
refused with it for the same reason: a stream is one record per line, and
indenting spreads a record over several.

Detection is unchanged, and it is what the first records wait for. jz
holds back until it has as many leading lines as the widest signature
among the parsers in scope looks at — twenty by default — because a
definition can rule itself out with a line further down, and choosing
before that would be guessing. Naming the parser narrows the scope, so
`jz run ping` and `COMMAND | jz --stream --parser mount` usually wait for
twenty lines and no more. The lines held back are then read by the same
code as everything after them.

`--extract` and `--exclude` apply to each record. The 64 MiB input limit
does not apply, since nothing is held; the 1 MiB limit on a single line
is what bounds a producer that never prints a separator.

### What --stream changes about the output

Everywhere else, standard output carries a complete JSON document or
nothing at all. With `--stream` that reads: every line of standard output
is a complete JSON document.

The second difference follows from the first. Reading a whole document,
one record that does not fit means the text is not the format it claimed
to be, so nothing is written and the status is 3. In a stream the records
already written have left, so jz cannot take that view: a record it
cannot read is reported on standard error as

```text
jz: ping/linux: line 42: line does not match /^\d+ bytes from/
```

and the records after it are still written. The status is 3 at the end if
anything was skipped. This is what a command that does not finish needs.
`ping` prints a request timeout among its replies and `rsync` prints
progress among its file names; ending the stream at the first of those
would throw away everything still to come.

Both readings say the same thing with exit status 3: something in the
input could not be read. What differs is how much of the rest survives,
and that is the whole of the difference.

Two failures are not records to skip and still end a stream at once. Text
whose format cannot be identified is exit 4 and is settled on the leading
lines, before a single record is written. A format that reads its whole
output into one object has no streaming form at all, which is exit 2.

### jz run --stream and the command's own status

`jz run` mirrors the status of the command it started, and it has one
number to return. When the command fails and a record was also skipped,
the command's status wins: it is the more useful signal, because a
command that failed explains both what it printed and what it did not.
The skipped records are on standard error either way, so nothing is lost
by the choice — only the number changes.

## Output jz has no definition for

`--define` takes the definition itself instead of the name of one. It is
a `parser.yaml` without the four keys that place a definition in a
registry — no `format`, `command`, `variant` or `detect` — so what is
left is `parse` and, if you want them, `input` and `fields`:

```console
$ docker ps | jz --define 'parse: {type: table, split: box}'
$ jz --define 'parse: {type: csv, delimiter: "|"}' --file export.txt
$ mytool --list | jz --define '
    input: {select: {after: "^---"}}
    parse: {type: kv, separator: ": "}'
```

Nothing is detected: you stated the format, so nothing can be chosen
wrongly. It cannot be combined with `--parser` or `--variant`, which
would be a second answer to a question already settled. A body that
cannot be read is exit 5 and the message names the key
(`--define: parse.type: unknown parse type "tabel"`).

The registry also carries a few definitions that describe a shape rather
than a command, for the same job under a name:

```console
$ docker ps | jz --parser table --variant box
$ jz --parser ini --file ~/.gitconfig
$ jz --parser csv --variant tab --file export.tsv
$ ss -tunlp | jz --parser table --variant whitespace
```

They are `table` (`whitespace`, `aligned`, `box`), `csv` (`comma`,
`tab`), `kv` (`colon`, `equals`) and `ini` (`default`). Automatic
detection never reaches them — two words above two words says nothing
about what produced them — and every value comes out as text, because a
shape says nothing about what its columns mean.

## Timestamps a format does not fully state

Two things a command prints cannot be read from the text alone.

`who`, `last` and `journalctl -o short` print `Nov  4 13:17` with no
year. `date`, `timedatectl` and `systemctl list-timers` name their zone
by an abbreviation, and `JST` is +0900 only if you carry a table of
abbreviations — the same three letters mean different offsets in
different parts of the world. Left alone, both stay the text they were
printed as:

```console
$ who | jz --extract time
[{"time":"Nov  4 13:17"}]
```

Supplying the missing half converts them:

```console
$ who | jz --assume-year 2025 --extract time
[{"time":"2025-11-04T13:17:00Z"}]

$ jz run --assume-year now who
$ jz run --assume-zone JST=+0900 --assume-zone CET=+0100 date
```

`--assume-year` takes a four-digit year or `now`, which is resolved once
when the command line is read. `--assume-zone` takes `ABBR=+HHMM` and may
be repeated.

They are assumptions and jz reports them as such: nothing is dated unless
you say so. Dating a bare `Nov  4` by this machine's clock, or reading
`JST` against this machine's own zone, would make the same text convert
differently on two machines, which is the opposite of what a converter is
for.

An assumption that no field in the chosen format asks for is not an
error. It changes nothing about the output, and refusing it would mean a
caller who writes one command line for several formats has to know which
of them prints a bare year. That is unlike a key named to `--extract`,
which changes the shape a consumer reads and so has to be right.

## Seeing what a definition extracted

A value that comes out wrong has two possible causes: the definition cut
it from the wrong place, or it converted it wrongly. Once the value is
typed the two look alike. `--raw` skips the field rules so that each
value is the text it was made from:

```console
$ df -h | jz --extract use_percent
[{"use_percent":92}]

$ df -h | jz --raw --extract use_percent
[{"use_percent":"92%"}]
```

Nothing is converted, `trim_prefix` and `null_if` are not applied, and
`required` and `when_missing` are left out with them — a shape that
changed with the values in it would defeat the point. Every value is a
string, except an empty aligned cell and a regex group that did not take
part in the match, which stay null.

It works with `--pretty`, `--stream`, `--extract` and `--exclude`. It is
not offered on `jz test`, which compares a fixture against the typed JSON
beside it.

## Seeing which parser was chosen

The choice is the one thing a converter has to get right, and a
successful run says nothing about it: the JSON looks the same whichever
definition produced it. `--explain` writes the answer on standard error,
one fact per line, each line opening with `jz: explain: `.

```console
$ jz --explain --file df-gnu.txt
jz: explain: chose df/gnu from embedded
jz: explain: scope: every definition in the registry, by its signature alone
jz: explain: matched: signature.all[0] /^Filesystem\s+1K-blocks\s+Used\s+Available\s+Use%\.../
jz: explain: rejected: tree/listing: signature.all[1] /^(?:\|-- |`-- )\S/ did not match
jz: explain: not considered: 48 definitions only used when named
jz: explain: read: 8 lines: 8 read
```

The first line is the outcome: `chose`, `unidentified` when no definition
fits, `ambiguous` when several do and nothing settles it, `mismatch` when
a named variant does not fit, or `defined` for `--define`. Then:

| Line | What it says |
|------|--------------|
| `scope` | which definitions were candidates, and what named them: nothing (the whole registry), `--parser`, the name of the command `jz run` started, or the path of `--file` |
| `matched` | each condition the chosen definition states and the input met |
| `settled by` / `outranked` | when several definitions fit, the rule that chose one (the registry layering, or `detect.priority` between variants of one command) and each one it chose over |
| `rejected` | a definition that came close and why it was ruled out |
| `held back` | a definition whose signature fits but which is only used when named |
| `not considered` | how many definitions are only used when named, and so took no part |
| `read` | where the lines of the input went: read, joined by `fold`, blank, or left out by each `input.ignore` expression |
| `command` | for `jz run`, the command and the status it gave |

`jz run` says the parser came from the command it started, and what the
system and the arguments narrowed:

```console
$ jz run --explain df -h
jz: explain: chose df/gnu-human from embedded
jz: explain: scope: the variants of df, from the name of the command jz ran
jz: explain: scope: narrowed by the system it ran on (linux) and its arguments (-h)
jz: explain: matched: signature.all[0] /^Filesystem\s+Size\s+Used\s+Avail\s+Use%\s+Mounted.../
jz: explain: matched: detect.os linux
jz: explain: matched: detect.args any [-h --human-readable]
jz: explain: rejected: df/bsd: signature.all[0] /^Filesystem\s+512-blocks\s+Used\s+Available\s+Capa.../ did not match
...
jz: explain: read: 12 lines: 12 read
jz: explain: command: df -h (exit 0)
```

Without a parser the search covers the whole registry, which rejects
almost all of it on the first expression of a signature. Saying so three
hundred times explains nothing, so only the definitions that came close
are listed: one that got past its first expression, and one whose
signature does fit but which jz will not choose on its own. A search
scoped to one parser lists every variant it left out. When nothing is
identified the ordinary message comes first and the explanation under it:

```console
$ jz --explain --file passwd.txt
jz: unable to identify the input format
it could be `etc` output, but that format is too generic for jz to claim on its own
...
jz: explain: unidentified: no definition fits the text
jz: explain: scope: every definition in the registry, by its signature alone
jz: explain: rejected: sensors/linux: signature.all[1] /^Adapter: \S/ did not match
jz: explain: held back: etc/passwd: its signature fits, but it is only used when named (--parser etc)
jz: explain: not considered: 47 definitions only used when named
```

`--explain=json` writes the same facts as one JSON document, on a single
line that opens the same way, so a script can take it apart from the rest
of standard error:

```console
$ jz --explain=json --file df-gnu.txt 2>&1 >/dev/null | sed -n 's/^jz: explain: //p' | jq -c '{outcome, chosen: .chosen.definition, read: .read.read}'
{"outcome":"chosen","chosen":"df/gnu","read":8}
```

Every key is there whatever the outcome, `null` or empty where it does
not apply: `outcome`, `scope` (`from`, `parser`, `variant`, `os`, `args`,
`path`, `path_dropped`), `chosen` (`definition`, `registry`, `matched`,
`settled_by`, `outranked`), `candidates` (the definitions an ambiguous
input fits), `rejected`, `held_back`, `not_considered`, `read` (`lines`,
`read`, `folded`, `blank`, `ignored`), `command` and `error` (`message`,
`exit`). The failure is still reported the ordinary way as well. In
`jz run` the command's own standard error shares the stream, which is
why the line opens with `jz: explain: ` rather than relying on being the
only thing there.

With `--stream` the explanation is written when the choice is made,
before the first record, so it has no `read` counts; a stream may never
end.

`--explain` changes neither standard output nor the exit status, and it
writes no clock reading, so two runs over the same input explain
themselves identically.

## What a file path says

`jz run` knows the command it started and its arguments. A file has no
argv, so its path is the only evidence besides the text: the directory
the file sits in names the parser, the file itself names the variant, and
the pair has to exist.

```console
$ jz --file /etc/fstab       # etc/fstab
$ jz --file /proc/meminfo    # proc/meminfo
```

Nothing in jz knows about `/etc` or `/proc`. Those directories work
because parsers named `etc` and `proc` exist, and a registry with a
parser named after some other directory gets the same treatment.

What this buys is the formats too unremarkable to claim on sight.
`/etc/fstab` is six whitespace-separated fields, which is why it declares
`auto_detect: false`; before the path was read it had to be named on the
command line every time.

The path is evidence, not an instruction. The definition it names still
has to fit the text, and when it does not — because its signature rules
the text out, or because it accepts the text and then cannot read it —
the path is dropped and the text is read on its own terms. So a path can
only add an answer, never replace one. A name with a dot in it is a file
name rather than a variant name and is ignored, which is what keeps a
capture saved as `fstab.txt` out of it.

The definitions that describe a shape are never named this way. They
carry no signature, so nothing about the text could rule one out and the
directory name would be the whole of the evidence. Naming those stays
something you do on the command line.

## Naming a parser

Detection settles most input on its own. Name a parser when it cannot:
a format too generic to claim, a wrapper whose name is not the tool it
runs, or a variant you want pinned in CI.

```console
$ du -a /etc/cron.d | jz --parser du
$ df -h | jz --parser df --variant gnu-human
$ jz run --parser df -- sudo df -h
```

`--variant` needs `--parser`: a variant name only identifies a definition
together with its parser. Naming either is a claim about the input, not a
way around the checks. The definition's signature still has to fit the
text, and jz fails if it does not; there is no option that turns that
off.

A wrapper is the common case for naming one. `jz run` takes the parser
from the name of the command it starts, so `sudo`, `env`, `nice`,
`timeout`, `stdbuf` and `busybox` all put their own name there instead of
the tool that produces the output. Name the parser and jz reads what the
wrapper ran:

```console
$ jz run --parser df -- sudo df -h
$ jz run --parser ps -- busybox ps
```

jz does not keep a list of which commands are wrappers. The list would
never be complete, and a wrong entry would read some other command's
output with the wrong definition.

A file is the other case. `/etc/fstab`, `/etc/passwd` and their
neighbours have no argv to detect them from, so they are variants of a
parser named `etc` and are always named:

```console
$ jz --parser etc --variant fstab --file /etc/fstab
$ cat /etc/nsswitch.conf | jz --parser etc
```

Naming `etc` without a variant is enough where the file recognises
itself; `jz list etc` prints the ones that exist.

The kernel's own files are read the same way, under a parser named
`proc`. Some of them say enough about themselves to be identified from
their text alone:

```console
$ cat /proc/meminfo | jz
[{"name":"MemTotal","value":64413356,"unit":"kB"}, ...,
 {"name":"HugePages_Total","value":0,"unit":null}, ...]

$ jz < /proc/loadavg
{"load_1m":0.8,"load_5m":0.56,"load_15m":0.42,"runnable":2,"total":4686,"last_pid":3717503}

$ jz --parser proc --variant uptime < /proc/uptime
{"uptime_seconds":1242659.1,"idle_seconds":38176926.97}
```

```console
$ cat /proc/cpuinfo | jz
[{"cpu":{"processor":0,"vendor_id":"AuthenticAMD","cpu family":26, ...,
         "flags":["fpu","vme","de", ...],"bugs":["spectre_v1", ...]}}, ...]

$ jz < /proc/diskstats
[{"major":7,"minor":0,"device":"loop0","reads_completed":16, ...,"flush_ms":0}, ...]
```

`/proc/meminfo` is a list of counters rather than one object keyed by
label, because which labels the kernel prints depends on how it was
built. The unit is a field of its own and is `null` on the four
`HugePages_` counters, which the kernel prints without one; the number
is left in the unit the file states rather than multiplied out.

`/proc/cpuinfo` is read under the variant `cpuinfo-x86`, because the
file has no shape common to the architectures: an arm64 machine writes
`Features` and `CPU implementer` where x86 writes `flags` and
`vendor_id`, and gets exit 4 here until somebody captures that form and
writes its variant. Within x86 the labels still vary by vendor, so a
block is read as whatever labels it holds, with a conversion for the
ones whose type is known and the kernel's own text for the rest.

`/proc/diskstats` is read for the twenty-field row Linux 5.5 and later
write. A fourteen- or eighteen-field row from an older kernel is refused
as a row with too few fields: nothing shifts and no counter is filled in.
Those shapes are not covered because there was no machine running such a
kernel to capture from.

`/proc/uptime` is the third case rather than the first two, and it is
worth saying why. It holds two decimal numbers and nothing else, which
is the shape of any pair of measurements, so jz will not claim it on
sight; naming the parser is what says which file this is. The signature
is still checked when you do, so naming it is not a way past the
checks.

## Where jz stops and the command begins

Everything from the command name onwards belongs to the command, so its
own options never reach jz. A bare `--` states the boundary explicitly:

```console
$ jz run ps aux
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

Three things are checked. Every `testdata/<case>.txt` is parsed with its
own definition and compared with the `.json` beside it; every fixture is
changed (a foreign line added, the whole text doubled) and must be refused
or show the change in its result, which is what proves the definition
reads all of its input; and every definition is then named explicitly on
every other definition's fixtures and must refuse them. The official fixtures travel inside the binary, so the
second check covers your definitions against every format jz already
reads without a copy of the repository: a signature wide enough to read
`df` output fails here rather than in someone's pipeline.

`--update` writes only to the registries under test; the built-in one
cannot be written to, so `jz test --update` with no directory writes to
the user registry and says on standard error where it wrote.

Failures go to standard error, one per line, followed by
`N passed, M failed`. Standard output stays empty. The exit status is 0
when everything passed, 1 when something failed, 2 for a usage error and
5 when a registry could not be read.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | unexpected failure (I/O, internal) |
| 2 | usage error |
| 3 | the input did not match the chosen definition, or held text the definition did not read |
| 4 | the format could not be identified, several matched, or a named one did not fit |
| 5 | a registry could not be loaded |
| 141 | standard output was closed early, as by a pipe into `head`; nothing is said, and a command `jz run` started is stopped |
| *n* | `jz run` mirrors the command's own non-zero status, or 128+signal when it was killed |

Diagnostics go to standard error with a `jz:` prefix. Standard output
carries a complete JSON document or nothing at all: a failure never
leaves half a document behind, so a consumer downstream sees valid JSON
or an empty stream.

## Registries

Definitions come from layered registries, and the first one that defines
a command and variant wins:

1. directories in `JSONIZE_REGISTRY_PATH`, separated the way `PATH` is
2. the user registry: `$XDG_CONFIG_HOME/jsonize/registry` on Linux,
   `~/Library/Application Support/jsonize/registry` on macOS,
   `%AppData%\jsonize\registry` on Windows
3. the registry built into the binary

`jz list --sources` prints them with what exists on your machine. jz
never accesses the network, so the same input converts to the same JSON
on the same machine.

A registry's `registry.yaml` can also switch definitions of the
registries below it off:

```yaml
format: 1
name: mine
disable:
  - file/posix   # one definition
  - du           # every variant of a command
```

A disabled definition is not loaded at all, so `jz list`, `--parser` and
automatic detection all stop seeing it. Shadowing replaces a definition
and needs a whole one written under the same name; disabling takes one
out. An entry that names nothing is a warning, not an error.

The order settles more than definitions of the same name. When automatic
detection is left with definitions of different commands that all fit the
text, the one from the earlier registry is the answer, because that order
is what you declared. A definition of your own can therefore take over a
format jz already reads, and it cannot make jz stop reading one. Two
definitions of the *same* registry that both fit stay an error naming
them; `jz test` reports those before they reach a pipeline.
