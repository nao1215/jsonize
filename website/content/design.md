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
internal/conformance      golden cases, tampered fixtures and the definition/fixture cross product, shared by `go test` and `jz test`
internal/schema           the JSON Schema of a definition's output, its validation and the compatibility rules
internal/buildinfo        the version string stamped at build time
pkg/registry              load registry directories/FS, merge with precedence, testdata cases
pkg/definition            YAML schema, validation, regex compilation, format checks
pkg/selector              variant selection (os / args / signature filter → precedence → priority)
pkg/engine                parse algorithms (table, csv, ini, tree, regex, kv, composite, records), streaming, field conversion
pkg/convert               scalar conversions (int, float, bool, size with units, time, duration)
pkg/jsonutil              insertion-ordered JSON object and encoder
registry/                 the official definitions and fixtures (data) + a one-file embed
e2e/atago                 end-to-end scenarios (e2e/README.md says what they guarantee where)
```

Data flows in one direction: capture → select → parse → encode. The
selector needs only the first lines of the text; the engine needs the
compiled definition and the whole text; encoding never sees definitions.

## Decisions and trade-offs

### A small public surface

The command line is four subcommands (`run`, `list`, `test`, `version`)
and the options listed in the usage page. Every option that asked the
user to make a decision jz should be making, or that changed the output
contract, was removed before release:

| Removed | Why |
|---------|-----|
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

`table`, `csv`, `ini`, `tree`, `regex`, `kv`, `composite` and `records`
cover the shapes the registered commands print. A composable step
pipeline (split → map → filter …) was considered and rejected: it is
harder to validate, harder to explain, and every real example so far fits
one of the eight. The `input.select` block (after/until/skip/limit) plus
`composite` gives section handling without a pipeline, `records` applies
one description to a block that repeats, and a `composite` part may be
`records` or `tree` so that a report with a header block above the
repeats has a shape too. Adding a type does not change the format
version, because an unknown type is already an error.

The list grew from five to eight, and each of the three broke the
assumption the first five share, that one line is one record: a CSV value
may contain a line break, an ini file is sections rather than lines, and
a drawn table marks its rows with rules. That is the test a ninth would
have to pass.

### The document formats stay out

XML, YAML, TOML, plist, x509 and JWT are not what commands print, and
each already has a tool whose whole job is that format: `yq`,
`openssl x509 -text`, `plutil`. A converter for command output that also
half-read six document formats would be worse at both jobs, and the
half-reading is what it would be: those formats have specifications, and
matching one is a project rather than a parse type.

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

### The depth is input-derived, the shape is not

`lspci -vv` prints a device, its capabilities under it and a capability's
flags under those. `lsusb -v` prints a device, a configuration, an
interface, an endpoint and an endpoint's attributes. How far that goes is
a property of the machine being described, so no definition can state it,
and this was out of scope for exactly that reason: a definition says what
the output looks like, and here it cannot say all of it.

What it can say is what a node is. `type: tree` splits the two: the
definition states the fields read from a node's line and that its
children go in a `children` array, and the input states nothing but how
deep the nodes go. The result has the shape the definition described,
repeated as many times as the text nests. That is a narrower thing than
"recursion in the format" and it is the part the earlier answer threw out
with the rest.

Every line is a node. Grouping runs of lines at one depth into a single
node was the alternative, and it makes a node's identity depend on its
siblings, which is a rule that cannot be stated in one sentence. With one
line per node, a node's line is read by `node.parse` — an ordered list of
regular expressions, or a key/value split — and the same description
applies at every depth, since the shapes that appear at each are what the
alternatives are for.

Three limits keep the input from deciding more than the depth. A line
indented two levels below the one above it has no parent and is an error
rather than being attached to the nearest ancestor. Indentation that is
not a whole number of the stated unit is an error rather than being
rounded down. Depth stops at 32, which no report comes near, so a
producer cannot make jz build an unbounded stack of objects.

`indent` states one level as it is written, and it accepts a list of the
forms one level may take. That second half was not in the first version
and three reports asked for it: tree(1) writes a level as one of
`"|   "`, `"    "`, `"|-- "` and ``"`-- "``, `systemd-analyze
critical-chain` as two spaces or a backtick and a dash, and `wpctl` as
four spaces or a branch character. Each is a fixed width whose characters
depend on whether anything follows, so a single string cannot count them
and a list of alternatives can.

A `level` expression that captured the leading whitespace was the other
candidate. It was dropped because it still leaves the question the width
answers — how much of what it captured is one level — so it adds a key
without removing a question.

`tree` may be a `composite` part, which is what a report with a banner
above the tree needs, and a `records` part, which `sensors -u` needs: it
prints a block per chip and a two-level tree of features inside each. It
was forbidden there at first on the grounds that a record and a tree both
say where a line belongs; they do not say it about the same thing.
`start` decides where a block begins and `indent` decides how deep a line
inside one is, and neither can answer the other's question.

### A definition can be given instead of named

jz reads output it has a definition for, and the honest answer for
everything else was exit 4 and a suggestion to write one. That is right
about what jz will not guess and wrong about what it will not do: the
caller often knows the shape perfectly well and only wants this reading
of it, once, without a registry.

`--define` takes the definition itself. It is a `parser.yaml` without the
four keys that place one in a registry, so what is left is `parse` and
optionally `input` and `fields`. Nothing is detected, which is why it
cannot be combined with `--parser`: both are answers to a question, and
`--define` has already answered it.

It adds no expressive power. The body goes through the same loader and
the same validation as a file, so an inline definition can say nothing a
file cannot, and a definition that turns out to be worth keeping is
moved into a registry unchanged.

Beside it the registry gained a few definitions that describe a shape
rather than a command: `table/whitespace`, `table/aligned`, `table/box`,
`csv/comma`, `csv/tab`, `kv/colon`, `kv/equals` and `ini/default`. They
are `--define` under a name, for the readings common enough to be worth
one.

Those needed a rule the registry had not needed before. A definition with
no signature and `auto_detect: false` makes no claim about the text at
all, so the checks that ask whether a definition reads a neighbour's
output cannot be applied to it: it reads everything of that shape, which
is what it is for, and running the check would report every fixture in
the registry against `table/whitespace`. `selector.ShapeOnly` names that
class, and the conformance runner leaves those out of the cross product
and requires the variant to be named rather than the parser alone, since
`table/whitespace` and `table/aligned` read the same lines two ways and
no text can separate them.

### Three formats that are not line-oriented

`csv`, `ini` and `table` with `split: box` were added because each one
breaks the assumption the other parsers are built on, that one line is
one record.

A CSV value may contain the delimiter, a quote, or a line break, so the
lines are joined back together and read with `encoding/csv`. A streamed
CSV holds a line back while the quote count is odd, which is exactly when
a value is still open. An ini file is sections of keys and so is one
object with no streaming form at all. A drawn table takes its cells from
the bars rather than from whitespace, and a value the table wrapped over
three lines is one cell.

What a drawn table does not do is mark every row with a rule. It was
written that way first, and `duf` said otherwise: MySQL, psql, `sqlite3`
in box mode and `duf` all draw a rule around the table and under the
header and nowhere else, so every line of the body is a row. A wrapped
value writes its continuation with the first cell left empty, and that is
what separates the two. It also means a row whose first column is
genuinely blank cannot be told from a continuation — in this format they
are the same line.

Adding them cost no new key beyond `split: box`, and each brought its own
fuzz coverage. That found a bug older than any of them: `StripANSI` would
take a line break as the final byte of an escape sequence, joining two
records into one, and it did so only in the whole-document reader —
the streaming one had already cut the line off. The two readings
disagreed on `"\x1b\n"`. No escape sequence ends with a line break, so
none may eat one.

### A repeating report is an array of samples

`iostat 1 3` prints its column header once per sample. The second one
reached the device table as a row and failed to convert, so the whole
report was exit 3 — the one input `iostat` is most often asked for could
not be read at all. `mpstat` and `pidstat` had the same fault.

Those reports are now read as an array of samples: `type: records` with
the sample's own header line as `start`, and the tables inside a sample
as its parts. A single-shot run is the same shape with one sample in it.
That is a breaking change to what those definitions return, and it is
the shape that makes `--stream` mean something here: one sample per line,
written as soon as it is complete, which is what a command run for
minutes is run for.

No new key was needed. The suspicion was that a table inside a repeating
block could not take its header from the block, which would have wanted
something like `header.from_block: true`. It can: a part's table already
derives its header from the first line of the part's region, and inside
`records` that region is the block. The vocabulary that was already there
(`records`, `composite` parts, `input.select.after`) said the whole
thing.

Two things were given up. The sysstat banner is dropped: it names the
kernel, the host and the CPU count, which `uname -a` and `nproc` also
print and jz also reads, and what it does not carry is a sample. Keeping
it would mean one object for the banner and an array for the rest, which
has no streaming form at all, so the trade is the banner against the
option this whole change exists for.

The other is that `vmstat` and `sar` were left alone. They print their
column header once and every sample under it, so an interval run was
already one table with one row per sample and nothing was broken. Making
them look like the others would have been a change for symmetry, and the
difference is in what the commands print rather than in what jz prefers.

### A duration is seconds, and the rounded rule is about naming

`ps` prints `00:04:14`, `uptime` prints `13 days, 4:22` and `w` prints
`13:42m`. Each of those is a length of time that a consumer has to
subtract, compare or sum, and doing any of that on the string means
reimplementing this reading in whatever asked. `type: duration` reads
them and reports seconds.

The one thing the text cannot settle is a bare two-part reading. `4:50`
is four minutes fifty seconds out of `ps` and one line later `1:23` is an
hour and twenty-three minutes out of `uptime`; nothing distinguishes
them. So `layout` is required and says which — `h:mm` or `mm:ss` — and
there is no default, because being wrong is a factor of sixty that
nothing downstream would notice. A trailing unit letter settles it where
the format prints one, which is how `w` separates `13:42m` from `13:42`.

The rule that a rounded number stays text applies here too, and working
out what it means sharpened the rule itself. It is about what the text
*names*, not about how coarse the value is. `1.8T` names no particular
number of bytes until a base and a precision are chosen for it, so
converting invents both. `13 days, 4:22` names exactly 1138920 seconds;
the machine has been up for some seconds more, but the text is not
ambiguous about the length it states, it just states a coarser one.

What that does rule out is a column whose shape changes with the value.
`top`'s TIME+ prints `mmm:ss.hh` and falls back to `hhh,mm` for a
long-running task, so one field would carry seconds for one row and
minutes for another. It stays text, and the comma form is deliberately
not a spelling `duration` reads: admitting a comma into the grammar would
make every other value containing one ambiguous, to serve one column of
one program.

### Assumptions are stated on the command line, never inferred

Two things a command prints cannot be read from the text alone. `who`
prints `Nov  4 13:17` with no year, and `date` prints `JST`, which is
+0900 only if you carry a table of abbreviations — the same three letters
name different offsets in different parts of the world.

Until now both stayed text, and a layout without a year was a validation
error so that nothing could be dated by accident. That was right about
the danger and wrong about the conclusion: the caller often does know
which year a log covers and what zone the machine was in, and jz had no
way to be told.

So the knowledge is asked for and never inferred. `--assume-year 2025`
(or `now`) dates a field whose definition says `year: assumed`;
`--assume-zone JST=+0900`, repeatable, gives an abbreviation an offset.
Without them the value stays the string it was printed as, and
`year: assumed` on its own converts nothing — it is a statement about the
format, which is why it is in the definition, next to the claim that the
layout is what it is.

Nothing reads a clock or a zone database on the way past. `now` is
resolved once when the command line is read, and whatever Go made of an
abbreviation is discarded rather than trusted, because Go resolves one
against the running machine's own zone and the same text would then
convert differently on two machines.

An assumption that no field in the chosen format asks for is not an
error. It changes nothing about the output, and refusing it would mean a
caller who writes one command line for several formats has to know which
of them prints a bare year. That is unlike a key named to `--extract`,
which changes the shape a consumer reads and so has to be right.

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

### --raw came back, for a different reason

`--raw` was removed before release as a second output shape for the same
input: two answers to the same question, and the typed one is the
contract. That reasoning was about a consumer choosing a shape, and it
still holds for one.

What it missed is the other reader. When a value comes out wrong there
are two possible causes — the definition cut it from the wrong place, or
it converted it wrongly — and once the value is typed they look alike. A
`use_percent` of 92 is right; `"92%"` cut down to 92 and the string `92`
somewhere else cut down to 92 are the same 92. `--raw` shows the text
each value was made from, so the two separate.

So it is back with a narrower job. It skips the field rules and nothing
else: no conversion, no `trim_prefix`, no `null_if`, and no `required` or
`when_missing` either, since a shape that changed with the values in it
would defeat the point. Every value is a string, or null for an empty
aligned cell and a regex group that did not take part. It is not offered
on `jz test`, which compares a fixture against the typed JSON beside it
and has nothing to say about the untyped reading.

### The choice is visible

Making the chosen parser and the reason for it visible is one of the
goals, and until `--explain` a successful run met none of it: the JSON
looked the same whichever definition produced it, and the only way to
find out was to read the registry. The selector already carried the
answer — it counts what it scanned and records why each candidate was
left out, because the failure messages are built from it — so what was
missing was a way to ask on the way past.

Everything it writes goes to standard error and nothing about the answer
changes, which is what lets it be added to a working pipeline. It writes
no clock reading either: an explanation is for comparing between runs and
pasting into a report, and a timestamp would make two identical answers
differ.

The rejections need a rule. A search of the whole registry rejects three
hundred definitions on the first expression of a signature, and printing
that is not an explanation. A search scoped to one parser has a handful
of variants and all of them are worth reading. So the line is drawn at
whether the definition met part of what it asks for: past its first
signature expression, or a signature that fits and an `auto_detect: false`
holding it back. Those are the near misses, and a scoped search reports
every variant because in that scope they all are.

What the first version left out was everything between "which one" and
"why that one". It said which definition was chosen and what it matched,
and it did not say what the candidates had been (the whole registry, a
parser named on the command line, the command `jz run` started, a file's
path), whether several definitions had fit and which rule had settled
it, how many definitions had taken no part because they are only used
when named, or where the input's lines had gone. Each of those is a
question someone debugging a wrong answer has to ask, and each answer was
already in hand: the selector records the rule that settled a tie as it
applies it, and the engine's ledger counts what `ignore` left out. So
the explanation now has a line for each, and the first line says which of
the outcomes it was: `chose`, `unidentified`, `ambiguous`, `mismatch`,
or `defined`. No and several are different failures with different
remedies — write or name a definition, or settle two that overlap — and
the exit status (4 for both) does not tell them apart.

There is no score. A confidence number would be one more thing to trust
without being able to check; every line here names a rule a definition
states or a rule of the selector, and can be checked against `jz list`.

`--explain=json` is the same facts for a script, one document on one line
that opens `jz: explain: ` like the text lines do. It goes to standard
error, not standard output, because standard output carries the
conversion and nothing else. The prefix is kept because `jz run` passes
the command's own standard error through on the same stream, and a line
a script can pick out is worth more than a stream it has to trust to be
clean. Every key is present whatever the outcome, so a consumer reads
every explanation the same way.

### A file path is evidence

`jz run` narrows the variants with the command name and its arguments,
and a pipe has nothing but the text. A file sits between the two: it has
no argv, but it has a path, and the path says something the text does
not.

The rule is one sentence and holds no list of names. The directory the
file sits in names the parser, the file names the variant, and the pair
has to exist in the registry. `/etc/fstab` and `/proc/meminfo` work
because parsers named `etc` and `proc` exist, which they do because those
formats were named after the directory they live in; a registry that
names a parser after some other directory gets the same treatment, and
nothing in Go knows about either path.

What it buys is exactly the formats a signature cannot claim. `/etc/fstab`
is six whitespace-separated fields, so it declares `auto_detect: false`
and, before this, had to be named on the command line every time — for a
file whose path already said what it was.

It stays evidence and does not become an instruction. The definition it
names has to satisfy its signature and then read the text; failing either
drops the pair and the text is read on its own terms, so a path can only
add an answer and never replace one. A base name containing a dot is
treated as a file name rather than a variant name, which keeps a capture
saved as `fstab.txt` out of it and stops the rule needing a list of
extensions.

The definitions that describe a shape are excluded. They were not at
first, and that was the one failure this tool exists to prevent: a
shape carries no signature, so nothing about the text can rule it out,
and `df` output in a file called `comma` inside a directory called `csv`
was read as CSV and returned at exit 0. The rest of the rule survives
because every other definition makes a claim the text can refuse; a shape
makes none, so for those the directory name would have been the whole of
the evidence. Naming one stays a claim the caller makes rather than one
their directory layout makes for them.

The alternative considered was reading the base name as the parser
(`df.txt` → `df`). It was rejected because it guesses where this rule
looks up: a file called `ls` in a directory called `bin` would name a
parser that exists, with no variant to confirm it against, and the whole
point of the pair is that both halves have to be right.

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
every *line* is a complete document.

That is why it is an option. Making it the default would mean giving up
the guarantee that a failure leaves nothing behind, for every user, to
serve the commands that need it.

### A stream refuses records, not the stream

Reading a whole document, one record that does not fit means the text is
not the format it claimed to be: nothing is written and the status is 3.
A stream cannot take that view, because the records already written have
left. So it takes the other one: a record jz cannot read is named on
standard error with its line number, the records after it are still
written, and the status is 3 at the end if anything was skipped.

This is what the commands `--stream` exists for actually print. `ping`
puts a request timeout among its replies, `rsync` puts progress among its
file names, and a monitoring loop runs for hours. Ending at the first
line with no reading for it would throw away everything still to come,
which is a worse answer than a partial one for exactly the inputs the
option is about. Batch mode is unchanged, so the guarantee is still there
for everyone not asking for a stream.

Two failures are not records and still end a stream at once. Text whose
format cannot be identified is settled on the leading lines before
anything is written (exit 4), and a format that reads its whole output
into one object has no streaming form at all (exit 2). Neither is a
record to leave out.

`engine.Stream` takes an `onError func(*ParseError) error` beside `emit`,
so a library caller chooses: returning nil carries on and leaves the
record out, returning an error ends the read there. A nil `onError` is
the second, which is what the conformance runner passes — a fixture read
whole and read as a stream have to agree record for record, and a stream
that quietly skipped one would hide the disagreement.

When `jz run --stream` skips a record and the command also fails, the
command's status wins. jz has one number to return and mirrors the
command's, which is the more useful of the two; the skipped records are
on standard error either way, so only the number is given up.

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

### Every line is accounted for

"Never produce plausible-looking JSON from the wrong parser" was only half
of the goal it came from. The other half is JSON from the right parser
that is missing something, and until this was built nothing stood in its
way. Two `dig` replies in one capture read as the first reply at exit 0:
the header part matched the first header, the answers stopped at the
first section break, and the second reply fell between the parts. `pactl
list sinks` lost every property, port and format of every sink the same
way, because the part that read the attributes named those lines as
belonging to a sibling that did not exist. Golden files could not show
either: they hold what a definition read, and a line it skipped leaves no
trace in them.

So the engine keeps a ledger. A line is read when a parser turned it into
part of the result, left out when a rule the definition states says so
(`ignore`, a blank line, a `fold` continuation), or it is unread, and
unread text is exit 3 with the line quoted rather than a smaller answer.
The rule lives in the engine and not in the definitions, so a definition
gets it without asking and cannot opt out. None of the 355 definitions
had to be edited to acquire it; 37 of them then failed on their own
fixtures, most for a heading they had never stated in full, and the
checks below found two more that only real output exposed.

The finer rules are the ones the first measurement asked for:

- A pattern has to reach both ends of the line it reads. The alternative,
  counting a line read when a pattern matched part of it, would pass
  exactly the case the ledger is for: a trailing field nobody asked
  about. Checking every fixture found two lines in the whole registry
  that fell foul of it, so it costs nothing to hold.
- The heading `select.after` matches counts as read only when the
  expression states the whole line. `after` is written to find a
  position, and `'^Features for '` was never a statement that the
  interface name after it is worthless. Stating the line is.
- A part's `ignore` hands a line to a sibling; it does not drop it. The
  documentation already said that was its purpose and nothing enforced
  it, which is how `pactl` lost its properties.
- A key printed twice into an object is an error, whatever the two
  values. An object keeps one of them, and two copies of the same value
  are what two documents read as one look like.
- `ignore` is the one way to leave text out on purpose, and it is
  trusted: what it names is what the definition declares worthless. That
  trust is checked from outside. `jz test` adds a foreign line to every
  fixture, at the end and in the middle, and reads every fixture twice
  over; a definition that succeeds must carry the line in its result or
  return more than one copy. That found `upower`'s catch-all
  `ignore: ['^[^:]*$']`, which had been dropping the device type and the
  history rows of a battery.

Blank lines are never unread. They carry no value that could be missing,
and a definition that keeps them for a parser that wants them
(`skip_blank: false`) should not have to name the ones it does not.

What this does not guarantee is a right reading. A table with explicit
`header.columns` takes its first line as the header without checking its
words; the signature is what checks them, and only inside its window. A
pattern that spans lines with `[^\n]*` or `(?s).*` reads what it spans
without reporting it; that is written into the definition, where a
reviewer can see it, and the tampering check catches one that swallows a
second copy of its input. The guarantee is narrower and exact: a
successful conversion did not skip any of its input without saying so.

Some text is left out on purpose, and saying so is now required rather
than implicit. The sysstat banner, `xrandr`'s Screen line and the count
under `tree` were dropped before and still are, for the reasons given
where each is decided; what changed is that each is stated in full, so
what is dropped is that line and not whatever happens to open like it.
Where the text carried something the result did not — the `dig`
question, EDNS options and warnings, `mtr`'s host, `pactl`'s properties,
ports and formats, `upower`'s device type and history — the definition
now reads it.

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

### The output has a contract, and it is derived

A consumer of `jz` output needs to know what it will get: which keys,
of which types, which are always there. The definitions already say it —
a column is a key, a named group is a key, a `type: int` field is an
integer, `when_missing: omit` is a key that may be absent — so the schema
is read off the definition rather than written beside it. A hand-written
schema would be a second statement of the same facts, and the first time
the two disagreed nobody would know which was right.

What the generator derives is what the engine guarantees and nothing
more. A key is required when every object of its kind carries it; a value
is nullable where the engine can leave it empty; a group that can only
match a few literals becomes an `enum`; a table that takes its column
names from its header says so with `additionalProperties` instead of
naming keys it cannot know. Every fixture in the registry is validated
against its schema, by jz's own validator and by an independent one in
CI; a disagreement is the generator and the engine disagreeing about a
definition, and it fails the build.

The schemas are published, in `registry/schemas` and on the site at the
`$id` each states, and each carries a contract version in
`x-jsonize.version`. That version is not `format`. `format` versions the
language a definition is written in; the contract versions what one
definition's output looks like. They change for unrelated reasons — the
`iostat` rewrite changed an output from an object to an array without
touching the language — and reusing one for the other would make both
mean nothing.

A change is breaking when a program reading the output, or a document
kept from before and validated against the new schema, can be broken by
it: a key removed, a type changed (made nullable included, and an object
become an array), a key that is no longer always there, a key that now
always is, an `enum` value added or taken away, an element type changed.
Each direction has a reader it breaks, so both are counted. The one change
that breaks neither is a key that may now appear and never has to.

A breaking change is refused unless it is meant. `make
registry-update-schema` rewrites the files and stops at a break;
`BREAKING=command/variant` publishes it under the next version. CI then
compares the schemas against the branch being merged into, so a schema
edited by hand to match a break, with the version left alone, is caught
there. Removing a definition ends its contract, which is said by listing
it in `registry/schemas/retired`.

The version is not written into the JSON jz prints. Every output would
grow a key its consumers did not ask for, and the ones that compare
documents would see every run differ from the last release's for no
reason in the data. The definition that read the text identifies the
contract, and `--explain` names it.

### Format versioning

`format: 1` is the major version of the definition language, and a
different number is rejected with a message that says whether to upgrade
jz or the definition. It says nothing about what a definition produces;
that is the output contract's version, above.

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

No JSON Schema library: the generator emits a small subset of draft
2020-12, and a validator for exactly that subset is shorter than the
dependency. CI checks the same fixtures with the Python `jsonschema`
package, so the two agreeing is evidence rather than one piece of code
agreeing with itself.

No CLI framework: four subcommands with a handful of flags each are
served by `flag` and a dispatch table, and the `run` subcommand needs
"stop at the first non-flag" semantics that `flag` gives for free.

## Deliberately out of scope for the MVP

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

### What a shape definition does about types

`table/whitespace` and its siblings convert nothing: every value comes
out as the text it was cut from, because a shape says nothing about what
its columns mean. That is right, and it leaves a gap. Someone naming one
of them usually does know what the columns mean, and has no way to say
so short of `--define` with the whole definition written out.

A `--field name=int` option would close it and is not obviously worth an
option: the same thing is one line of `jq`, and an option that converts
values is a second place where types are decided, next to the definitions
where they belong.

### Where the sysstat banner went

Reading `iostat` as an array of samples dropped the banner, which named
the kernel, the host, the architecture and the CPU count. The reason is
sound — a shape with one object for the banner and an array for the rest
has no streaming form, and the option is the point of the change — but
the information is gone rather than moved. `uname -a` and `nproc` print
the same facts and jz reads both, which is an answer for a script and not
for someone reading one command's output.

What would close it is a way for a definition to say that some fields
belong to every record of a stream. Nothing in the format says that
today, and inventing it for one report would be the wrong order.

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

### A column that prints two layouts

`ls -l` prints `Jan  5 10:11` for a file changed in the last six months
and `Apr  9  2025` for an older one. One column, two layouts, and which
one a row carries depends on the age of the file rather than on anything
the definition knows.

A field takes one layout, so the column stays text. `year: assumed`
does not help: it says the format prints no year, and this one prints a
year for some rows. Reading the value would mean either two more regex
alternatives in a definition that already has two, splitting on a
distinction nobody asked about, or a field that may state a list of
layouts and tries them in order.

The second is probably right and is not done, because a list of layouts
would be the first place in the format where a field guesses, and the
rule that keeps `--force` out of this tool is the same rule. The
question is whether trying layouts in a stated order counts as guessing
when the definition wrote the order down. `who`, `last` and
`journalctl -o short` all print one layout, so nothing in the registry
needs it today.

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
