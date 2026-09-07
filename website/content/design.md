---
title: Design
description: The decisions behind jsonize, the alternatives considered and what was left out.
toc: true
---

This document records the decisions behind jsonize, the alternatives that
were considered and what the MVP deliberately leaves out.

## Goals

- Turn command output into JSON without writing Go per command.
- Make the chosen parser and the reason for choosing it visible; never
  produce plausible-looking JSON from the wrong parser.
- Keep definitions safe to accept from third parties.
- Behave identically in a terminal, a pipeline and CI.

## Architecture

```
cmd/jz                    entry point: signals, exit code
internal/cli              subcommands, flag parsing, registry layering, exit-code contract
internal/runner           exec mode: child process, LC_ALL=C, stderr passthrough, output cap, signals
internal/conformance      golden cases and the definition/fixture cross product, shared by `go test` and `jz test`
internal/buildinfo        the version string stamped at build time
pkg/registry              load registry directories/FS, merge with precedence, testdata cases
pkg/definition            YAML schema, validation, regex compilation, format checks
pkg/selector              variant selection (os / args / signature filter → precedence → priority)
pkg/engine                parse algorithms (table, regex, kv, composite, records), streaming, field conversion
pkg/convert               scalar conversions (int, float, bool, size with units, time)
pkg/jsonutil              insertion-ordered JSON object and encoder
registry/                 the official definitions and fixtures (data) + a one-file embed
e2e/atago                 end-to-end scenarios
```

Data flows in one direction: capture → select → parse → encode. The
selector needs only the first lines of the text; the engine needs the
compiled definition and the whole text; encoding never sees definitions.

## Decisions and trade-offs

### A small public surface

The command line is four subcommands (`run`, `list`, `test`, `version`)
and six options (`--file`, `--pretty`, `--stream`, `--parser`,
`--variant`, `--help`). Every
option that asked the user to make a decision jz should be making, or
that changed the output contract, was removed before release:

| Removed | Why |
|---------|-----|
| `--raw` | a second output shape for the same input; the typed one is the contract |
| `--meta` | an envelope that changed the JSON schema depending on a flag |
| `--os` | asking the user to declare the producing system instead of matching the text; jz knows the system when it runs the command itself |
| `--force` | a way to parse with a definition whose signature says the text is something else, which is exactly the failure jz exists to prevent |
| `--max-input` | a safety limit, not a preference: 64 MiB, internal |
| `--embedded-only`, `--registry` | registry plumbing; `JSONIZE_REGISTRY_PATH` covers the real use and tests inject sources directly |

### Definitions in YAML rather than Go plugins or a scripting language

Per-command code is the obvious design and it is where most output
converters end up: hundreds of hand-written parsers whose variant
detection is ad-hoc string sniffing and whose schema lives in comments.
jsonize instead fixes a small declarative language. The price is
expressiveness: a format whose shape comes from the input rather than
from the definition, such as a tree of arbitrary depth, cannot be
expressed. The gain is that every definition is reviewable, testable by
a fixture, and safe to load from a third party. YAML was chosen over JSON (no
comments) and TOML (awkward nesting); `goccy/go-yaml` provides strict
decoding and line numbers in errors.

### A handful of parse types, not a general pipeline

`table`, `regex`, `kv`, `composite` and `records` cover the shapes the
registered commands print. A composable step pipeline (split → map →
filter …) was considered and rejected: it is harder to validate, harder
to explain, and every real example so far fits one of the five. The
`input.select` block (after/until/skip/limit) plus `composite` gives
section handling without a pipeline, `records` applies one description
to a block that repeats, and a `composite` part may be `records` so that
a report with a header block above the repeats has a shape too. Adding a
type does not change the format version, because an unknown type is
already an error.

### Table splitting

- `whitespace`: split on runs of whitespace with the last column
  absorbing the remainder. Used for `df`, `ps`, `free`.
- `aligned`: cut cells at the rune offsets where header words start,
  moving a boundary left when a right-aligned value is wider than its
  header and keeping a token whole when it overflows to the right. Empty
  cells become `null`. Needed for `lsblk` and `w`.
- `delimiter`: a literal separator (`du`'s tab).

Explicit `header.columns` is preferred in the official registry because
derived names depend on the exact header text; derivation exists for
quick local definitions.

### Variant = output format

One definition per *format*, not per implementation. GNU and BusyBox `df`
without options print the same table, so one definition covers both and
lists them in `metadata.compatible`; `df -h` differs between them ("Avail"
vs "Available") and gets two definitions. This keeps definitions
honest: a definition claims exactly what its fixtures prove.

### Rounded numbers stay text

`df -h` steps by 1024 and `df -H` by 1000; the output records neither,
and both round. Deriving bytes from "1.1G" would invent both a base and a
precision, so human-readable sizes are reported exactly as printed and
the exact forms of the same commands (`df`, `free`, `ls -l`, `lsblk -b`)
are what produce numbers. The same reasoning removed the `ls -l` /
`ls -lh` split: which one produced a listing is not decidable from the
text, and with the size kept as printed it does not need to be.

### Some formats are only used when named

A signature is a necessary condition, but a weak one can still be met by
unrelated text: a number, a tab and a path is `du` output and
`git diff --numstat` alike. Such a definition sets
`detect.auto_detect: false`, which keeps it out of automatic detection
while `--parser du` and `jz run du` still reach it, signature check
included. The alternative, adding exclusion patterns for every other
format that happens to look similar, is a list that can never be
finished.

### Signatures say what a format is

A signature is a claim about the text: this is the output of this
command. It is written as what must be true, and `none` is not how a
definition earns its answer.

The alternative was tried and it does not scale. `file/posix` reads
"path: description", which is the shape of a dozen other reports, so it
collected an exclusion for each one it met: `blkid`, `lscpu`,
`git stash list`, `udevadm`, `modinfo`, `nmcli`, `pip show`, a Debian
control record, `fc-list`, `ethtool -i`. Fourteen of them, and the
fifteenth was going to arrive with the next definition somebody added.
A list of everything a format is not cannot be finished, and each entry
made `file/posix` a definition other people had to edit before their own
would pass.

What replaced it says two things about file(1) output itself. Every line
is "path: description", which is where `apt show`, `dpkg -s`, `zipinfo`
and `lscpu` stop resembling it; and some description names a kind of
file, from a list this definition owns. Fourteen exclusions became zero,
and nothing about the neighbours is mentioned at all.

Four rules follow from that, and they are what a signature is reviewed
against:

- Write the shape, and write the whole of it. Anchoring both ends is
  what separates `uptime` from `w`, which opens with the same line, and
  `ipcs -q` from `ipcs`, which opens with the same section. `\A` and
  `\z` do most of the work that a `none` used to.
- A signature may narrow what the definition undertakes to read.
  `git log --oneline` is read for an abbreviated hash of seven to twenty
  digits, which leaves no room for a 32, 40 or 64 digit digest; the
  price is `--no-abbrev`, and the definition says so. This is a decision
  about scope, not a description of md5sum.
- Unknown is an answer. A format jz cannot claim exits 4, and that is
  the tool working. The failure to design against is the other one: a
  text read confidently with the wrong definition and returned with
  status 0.
- `none` is a safety valve for the case where there is no positive form
  to write, and the reason belongs beside it. One is left in the
  registry, in `etc/hosts`: `ip neighbour` prints an address, "dev", an
  interface and a state, and every word of that is a legal host name, so
  nothing in the text tells the two apart.

Definitions have to be independent for the registry to grow. Adding one
should need nothing but its own signature and its own fixtures; needing
to edit a definition somebody else wrote means the two were coupled
through an exclusion. Measuring that is one command: take a `none` out
and run `jz test`. Of the 68 the registry had, 31 excluded nothing at
all, 13 named another command, and 11 remain, every one of them telling
variants of a single command apart.

### Fixtures verify a signature, they do not decide it

The order is: decide what the format is, write the signature, then
capture fixtures that show it. The other order looks the same from the
outside and is not: "no fixture misdetected, so the signature is fine"
holds only until somebody adds a fixture, and it leaves the definition
saying whatever happened to work rather than what was meant.

`file/posix` carries the difference. Its fixtures are one line per kind
the signature names, a listing that mixes a kind with lines file(1)
could not read, and a listing that names no kind at all and is refused.
Each of the three exists because the signature says something, not the
other way round.

### Selection never guesses

Candidates are filtered by criteria that apply (a criterion whose input is
unknown, such as arguments in pipe mode, neither helps nor hurts). One
survivor is the answer; there is no ranking by how closely a definition
fits. Several survivors are settled by `detect.priority` only when they
are variants of the same command and one priority is strictly highest,
which is a statement the definition author made deliberately; anything
else is an error that names the candidates. The alternative, "first match
wins", would silently depend on directory order.

Survivors from different registries are settled before that, by the
layering: the definition from the earlier registry wins. This is a
declaration rather than a guess, and it is the same declaration that
already decides which definition of one command and variant applies. The
alternative was to keep it an error, which meant that one definition
someone added locally could take an official parser away from them: with
a `^Filesystem` signature in a user registry, `df -h | jz` reported that
the input matched several parsers and exited 4. Definitions inside one
registry are still never ranked against each other, so a collision the
registry owns stays an error they have to resolve. `jz test` is where
they see it.

Fixtures double as selection tests: every fixture is pushed through the
selector and must pick its own definition, and every definition is named
explicitly on every other definition's fixtures and must refuse them, so
adding a variant whose signature overlaps an existing one fails
`jz test` immediately.

### Exec mode forces `LC_ALL=C`

Output formats are documented for the C locale; translated headers and
localized numbers break parsers. jz sets `LC_ALL=C` and `LANG=C` (and
drops `LANGUAGE`) unless `--keep-locale` is given. Pipe mode cannot
control the producer, which is why signatures match structure rather than
prose where possible.

### Exit status of `jz run`

A failing command's status is mirrored, and its output is still parsed
when there is any (for example `df` exits 1 when a mount point is
unreadable but prints the table). jz's own codes (2–5) are documented and
distinct from 0/1 so scripts can tell them apart, but they can collide
with a child's codes; the stderr line `jz: <cmd> exited with status N`
disambiguates.

### Streaming is an option, not the default

A command that does not end (`ping`, `vmstat 1`) has no whole document to
write, so `--stream` writes one JSON document per line as each record is
read. It is the only option that changes the output contract: everywhere
else standard output carries a complete document or nothing, and here
every *line* is a complete document. A record that cannot be read still
stops the conversion with exit status 3, but the records already written
stay written, because they are finished documents that have already left.

That is why it is an option. Making it the default would mean giving up
the guarantee that a failure leaves nothing behind, for every user, to
serve the commands that need it.

Detection is unchanged and it is what the first record waits for. jz
holds back until it has as many leading lines as the widest signature
among the candidates looks at — twenty by default — because a definition
can rule itself out with a line further down, and committing before that
would be the guess the selector exists to avoid. Naming the parser, which
`jz run` always does, narrows the candidates and usually the wait with
them. The held lines are then read by the same code as the rest, so there
is one reading, not two: `jz test` checks every fixture both ways and
fails a definition whose two readings disagree.

Only a format that yields records can be streamed. `composite`,
`each: input` and a kv map build one object out of the whole text, and
that object does not exist until the last line has arrived; asking for it
one record at a time is a request jz cannot carry out, so it is a usage
error rather than a silent fallback.

### Registry layering and the code/data boundary

`registry/` holds only YAML, fixtures and one `embed.go`. `internal/*`
never imports it; only `cmd/jz` and the golden test do. Moving the
registry to its own repository means changing one import.

Layering (`JSONIZE_REGISTRY_PATH` → user registry → embedded) lets a user
fix a parser locally today and ship it upstream tomorrow with no change
in behaviour.

### No network, ever

jz reads local directories and nothing else. A release carries both the
code and the definitions, so a given input converts to the same JSON on a
given machine whatever the network is doing. Updating the official
registry means installing a new jz; adding your own means pointing
`JSONIZE_REGISTRY_PATH` at a directory.

### Format versioning

`format: 1` is the schema major version, and a different number is
rejected with a message that says whether to upgrade jz or the
definition.

Until the first tag, format 1 is not frozen: keys are added, renamed and
removed while the shape settles. After the tag, adding a key is a minor
release and the number stays 1. An older jz meeting a definition that
uses the new key refuses that definition with an unknown-key error saying
a newer jz may be needed, which is the whole compatibility story: a
definition works with the build it was written for and every later one.
Removing a key, or changing what one means, is `format: 2`, because
neither can be told from a mistake by looking at the file.

`min_jsonize` was the alternative, a version a definition could demand.
It was removed: it asked every definition author to know which release
introduced each key they used, and it said nothing at all when they got
it wrong. The unknown-key error needs no such bookkeeping, and it names
the key that is the problem.

### Dependencies

- `github.com/goccy/go-yaml` — strict YAML decoding with positions.
- `github.com/google/go-cmp` — structural diffs in golden failures.

No CLI framework: four subcommands with a handful of flags each are
served by `flag` and a dispatch table, and the `run` subcommand needs
"stop at the first non-flag" semantics that `flag` gives for free.

## Deliberately out of scope for the MVP

- Recursive sections, where the depth comes from the input (`ls -R`,
  `npm ls --all`, `docker info`). A block that repeats at one level is
  `records`, and a value continued on the next line is `input.fold`; a
  tree is neither.
- Derived fields (computing `uptime_seconds` from `"13 days, 4:30"`).
- Two commands that print the same format under both their names.
  Nothing in the text says which of them wrote it, so `vdir`, `getent`
  and `printenv` are left to `ls`, `etc` and `env`.
- Fetching or updating registries over the network.
- A JSON Schema for editor completion of `parser.yaml`; validation is
  done in Go with path-qualified messages instead.

### The library under pkg/

The six packages that read text are under `pkg/`, so another program can
load a registry, select a definition and parse with it without going
through the command line. `internal/` keeps what only the binary needs:
the subcommands, the child process, the conformance runner and the
version string.

The split is where it is because the exit-code contract, the flag parsing
and the process handling are decisions about a command line, not about
reading text, and a library that carried them would be answering
questions its caller has already answered. What crosses the line is the
registry, the definition schema, the selector, the engine, the scalar
conversions and the ordered object — everything between a piece of text
and the JSON for it.

The surface is kept small deliberately: what `cmd/jz` and the tests do
not reach is unexported, and what stays is what a caller cannot avoid
naming. The enum vocabularies of the definition schema
(`RecordNUL`, `UnitBinary`, `MissingNull` and their siblings) are the
exception: they name the values a caller reads out of a `Definition`, and
exporting half a set would be worse than exporting all of it.

## Open questions

Decisions that have not been made. Nothing here is implemented, and each
is recorded because a definition in the registry works around it today.

### Exact field counts in a table

A `table` with `split: whitespace` gives its last column whatever is left
on the line. That is what `ps aux` needs, where the command and its
arguments are one cell with spaces in it, and it is the default because
`max_fields` is unset. It means a table cannot say "this row has exactly
N fields": a row with N+1 of them produces N cells with two values in the
last one, and `max_fields: N+1` is refused by validation because it
exceeds the number of columns.

`proc/diskstats` needs the exact count and gets it from two places that
were never designed to work together. Inside the signature window the
expression counts the fields. Past the window nothing counts them, and
what refuses a twenty-first field is that it lands in `flush_ms` and
fails to convert to an integer. A table whose last column were text
would take the extra field without a word.

Two uses are being served by one setting, and they want opposite things:
a last column that absorbs the rest of the line, and a row whose width is
checked. Relaxing the `max_fields` validation, or adding a key that
states the width, would separate them. Neither is decided, and adding a
definition key is not something to do for one definition.

Two further things any answer has to handle:

- The widths a format allows are not always a range. `/proc/diskstats`
  has fourteen, eighteen and twenty fields across kernel versions, and a
  row of fifteen, sixteen, seventeen or nineteen is not any of them. A
  count written as a range would accept rows no kernel ever wrote.
- Field counts cannot resolve every ambiguity. A twenty-field row
  truncated to eighteen is indistinguishable from a row an older kernel
  wrote, because the columns are backward compatible and the text carries
  nothing else. Splitting the shapes into separate variants does not
  change that: automatic detection would have the same two candidates and
  the same absence of evidence.

### Whether a definition should split a value it can

A field of type `object` splits a value on a regular expression, so a
definition can turn `1024 KB` into a number and a unit, or
`48 bits physical, 48 bits virtual` into two numbers. That is reading
what the text states rather than guessing at it, which is why
`proc/loadavg` splits `4/4688` into a runnable count and a total.

The registry is not consistent about when to do it. `proc/meminfo`
separates every counter from its unit; `proc/cpuinfo-x86` reports
`cache size`, `TLB size` and `address sizes` as the text the kernel
wrote. Both are defensible on their own and the pair is not. What is
missing is a rule for which values a definition takes apart and which it
hands over whole, and until there is one, changing either would be
trading one inconsistency for another.

## Where to cut next

- `registry/` → separate repository; only the `official` import changes.
