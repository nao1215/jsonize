---
title: Definition format
description: "The YAML a parser definition is written in: detection, parsing and field conversion."
toc: true
---

A parser definition is one YAML document, `parser.yaml`, stored at
`parsers/<command>/<variant>/` inside a registry. The directory names
must match the `command` and `variant` keys. The registry itself is
described by [its manifest](#the-registry-manifest).

```yaml
format: 1                       # required; the schema version
command: df                     # required; [a-z0-9][a-z0-9._+-]*
variant: gnu-human              # required; [a-z0-9][a-z0-9-]*
aliases: [...]                  # optional; other commands printing this
description: GNU coreutils df -h
metadata: {...}                 # optional, informational
detect: {...}                   # optional; how to choose this variant
exec: {...}                     # optional; environment for jz run
input: {...}                    # optional; line pre-processing
parse: {...}                    # required; the algorithm
fields: {...}                   # optional; conversions per field
```

Unknown keys are errors naming the key and saying that a newer jz may
read it: keys are added within format 1, so this is how a definition
written for a later release reaches an older jz. Names such as `aux`,
`con` or `nul` are rejected because they cannot be directories on
Windows.

## aliases

More than one command can print the same format, and nothing in the text
says which of them wrote it. One definition answers for all of them:

```yaml
aliases:
  - name: vdir                  # required; the other command's name
    args: {}                    # optional; replaces detect.args here
  - name: getent
    args: {all: [passwd]}
  - name: podman                # inherits detect.args
```

`jz run vdir`, `jz --parser vdir` and `jz list vdir` all reach the
definition; `jz list` reports the aliases under the command they belong
to rather than as commands of their own, because they add no format.

`args` is what makes an alias more than a second name. An alias that says
nothing there is filtered the way the command is, which is what `gdf`
wants: it is GNU `df` under the name macOS installs it as, and `gdf -h`
should reach the same variant `df -h` does. An alias that needs other
arguments states them, which is what `getent passwd` wants. An alias that
states an empty filter accepts any arguments, which is what `vdir` wants:
it prints the long listing that `ls` prints only with `-l`.

## metadata

| Key | Meaning |
|-----|---------|
| `tags` | free-form strings shown by `jz list --json` |
| `references` | URLs (manuals, source) |
| `compatible` | implementations known to print this format, e.g. `[GNU coreutils, BusyBox]` |

## detect

```yaml
detect:
  os: [linux, darwin]            # GOOS values; empty = any
  args:
    any: ["-h", "--human-readable"]
    all: []
    none: ["-i"]
  signature:
    all: ['^Filesystem\s+Size']  # every expression must match
    any: []                      # at least one must match
    none: []                     # none may match
    window: 20                   # lines examined (default 20, max 200)
  priority: 0
  auto_detect: true              # false = only used when the parser is named
```

- `os` is compared with the running OS in exec mode. In pipe mode the
  producing OS is unknown and the criterion is skipped.
- `args` applies only in exec mode. Each entry is one whole word as it
  was typed, so `--format=long` is the word `--format=long` and a
  definition that means both spellings lists both. Bundled short flags
  are expanded, so `-hT` satisfies `any: ["-h"]`. A `--` ends the
  options: the words after it are operands whatever they look like, and
  no filter sees them, so `ls -- -l` lists a file named `-l` and reaches
  the variant that reads names.
- `signature` expressions are matched against the first `window` lines
  joined with newlines, in multi-line mode (`^`/`$` match line
  boundaries). `\A` anchors at the start of the input, and `\z` at the
  end of the window, which is the end of the input only when the input
  fits in it: a rule about every line holds for the lines the window
  holds.
- `auto_detect: false` marks a format whose text is not evidence on its
  own: three numbers in a row, or a number and a path, describe far too
  many things. Such a definition is skipped by automatic detection and
  used only when the parser is named (`--parser du`, or `jz run du`),
  where its signature is still checked.
- A definition with `auto_detect: false` and no signature at all reads
  any text of its shape. Under a command whose other variants have a
  signature (`ls/names` beside the long listings), naming the command
  on a pipe does not reach it, since it would take the output of an
  option the others refuse; it is used when its variant is named, or by
  `jz run`, where the arguments have narrowed the variants first. A
  command whose variants all lack one (`csv`, `table`) is unaffected.

### Writing a signature

A signature is a claim that the text is this command's output, so write
what must be true of it and let `none` alone.

Write the shape and write the whole of it. `\A` and `\z` anchor the
text rather than a line, and that is what separates a format from one
that opens the same way: `uptime` is one line and nothing after it,
where `w` continues into a table; `ipcs -q` is one section and its rows
to the end, where `ipcs` has two more. Inside the text, `^` and `$` are
line anchors, so a rule about every line is written as the whole text
made of those lines.

A signature may also narrow what the definition undertakes to read, and
that is a decision worth writing down rather than a shortcoming.
`git log --oneline` is read for an abbreviated hash of seven to twenty
digits, which leaves no room for a 32, 40 or 64 digit checksum listing;
the price is `git log --no-abbrev`, and the comment beside the
expression says so.

`none` is for the case where there is no positive form to write, and the
reason belongs beside it. It is not the way to keep a neighbouring
format out: a list of what a format is not cannot be finished, it grows
by one every time somebody adds a definition, and it couples your
definition to theirs. If you find yourself adding a `none` that names
another command, the signature above it is not yet saying what the
format is.

A format jz cannot claim is refused with exit 4, which is an answer.
The one to design against is the other: text read confidently with the
wrong definition and returned with status 0.

Selection: a candidate whose applicable criterion fails is rejected. One
remaining candidate is the answer. Several from different registries are
settled by the registry layering, the earlier one winning. Several from
one registry are settled by `priority` only when they are variants of the
same command and one priority is strictly highest; otherwise the
selection is an error naming them.

## exec

```yaml
exec:
  env:
    TZ: UTC
```

Extra environment for `jz run`. jz already sets `LC_ALL=C` and `LANG=C`.
When several variants of a command set the same variable to different
values the variable is not set at all (the variant is unknown before the
command runs).

## input

```yaml
input:
  record_separator: newline # or nul, for `env -0` style output
  fold: '^[ \t]+\S'         # join a wrapped line onto the one above it
  ignore: ['^total \d']    # drop matching lines
  skip_blank: true          # default true
  select:                  # applied in this order; see below for what
    after: '^BEGIN$'       #   start after the first match, the heading
    until: '^END'          #   stop before the first match
    skip: 1                #   pass over N leading lines
    limit: 50              #   keep at most N lines
```

The same four `select` keys narrow a `composite` part's region, which is
where `until`, `skip` and `limit` earn their keep: what they leave out of
one part is what its siblings read.

Input is split on the record separator, a newline by default. Each
record is then prepared in this order: the ANSI escape sequences come
off (a command that keeps colouring its output through a pipe would
otherwise hide its format, and its values, behind them), then the
trailing `\r` of a CRLF ending, then the UTF-8 BOM of the first record.
The order is the same for detection, for the whole document and for
`--stream`, so a mark behind a colour code is a mark in every reading.
The 1 MiB limit on a record counts the bytes between two separators as
they were read, the escapes and the `\r` included, and the last record
of an input is held to it whether or not a separator follows it. Blank
lines before the first line of text and after the last are not part of
it, whatever `skip_blank` says. With `skip_blank: false` only an empty
line counts as blank there, since a definition that keeps blank lines
may be reading lines of spaces as values (`ls/names` reads a file named
with spaces); a blank line the definition keeps is still a line
`ignore` may name.

`fold` names the continuation of the line above it. A matching line is
joined onto the previous one with a single space and its own leading and
trailing whitespace removed, and the joined line keeps the number of the
line it started on. What `--stream` holds while it waits for a record to
end (a fold, a `records` block, a tree node with its children, a quoted
csv value, a composite part's region) is bounded by the input limit, and
a record that grows past it ends the stream. It runs before `ignore` and `skip_blank`, so a
continuation is joined even where the line it belongs to would be
dropped. A continuation with nothing above it is an error naming the
line, rather than something quietly dropped. This is for a report that
breaks a long value at the terminal width (`ethtool` listing link modes)
and for the control files whose values continue on an indented line.

With `record_separator: nul` the records are separated by NUL bytes instead,
which is what makes a value containing a newline representable. Input
must be valid UTF-8 and within the size limits. A format read line by line
refuses text holding a NUL byte, both when it is detected and when it is
named: that is the output of a command run with `-z` or `--zero`, one line
holding every record, and its last field would take all of them.

### Every line is read or left out by a rule

A conversion that succeeds has read all of its input. Every line ends up
in one of these places, and a line in none of them is an error naming it
(exit 3), not a shorter result:

- read by a parser: a table row or header, a line a pattern matched, a
  key/value line, a tree node;
- joined onto the line above by `fold`;
- left out because it is blank, or because an `ignore` expression names
  it;
- the heading of a region: the line `select.after` matched, when the
  expression describes the whole of it.

`ignore` is the one way to leave text out on purpose, so what it names is
what the definition declares worthless: a legend, a column header the
parts do not need, a count that restates the rows. `jz --explain` reports
how many lines each expression took.

`select` narrows the lines a parser is given. At the top level nothing
else is given the rest, so every line it leaves out has to be blank or
ignored; in a `composite` part, what one part's region leaves out is read
by its siblings or is unread. The heading `after` matches counts as read
only when the expression states the line from end to end (surrounding
whitespace aside): `after: '^Features for \S+:$'` does, `after: '^Features
for '` leaves the rest of the line, and with it the interface name, unread.

## parse

### type: table

```yaml
parse:
  type: table
  split: whitespace | aligned | delimiter | box   # default whitespace
  delimiter: "\t"                            # with split: delimiter
  header:
    columns: [filesystem, size, used]        # explicit names
    none: true                               # no header line (columns required)
    leading_label: type                      # name for an unlabelled first column
    rename: {login: login_at}                # rename derived names
    repeated: true                           # a body line equal to the header starts another table (default true)
  max_fields: 6                              # whitespace/delimiter: last cell absorbs the rest
  min_fields: 3                              # rows with fewer cells are errors (default: column count)
```

Result: an array of objects, one per row, keys in column order. Without
`header.columns` the names are derived from the header: lower-cased,
non-alphanumerics become `_`, a leading or trailing `%` becomes
`_percent` (`%CPU` → `cpu_percent`, `Use%` → `use_percent`,
`1K-blocks` → `1k_blocks`, `Mounted on` → `mounted_on`).

A line of the body that repeats the header, word for word (cell for
cell with `split: delimiter`), starts a second table: the output of the
command run twice, or two files joined. It is read as the header again,
which with `split: aligned` says where the second table's columns are,
and never as a row of column names. `box` does the same with a row equal
to the header row. That is what `header.repeated` says, and it is the
default for a table, since a command that prints a report per interval
prints its header with each; `repeated: false` makes such a line a row,
for a table whose cells may hold the column names. A `csv` defaults the
other way (below). A table with `header.none` has no header to repeat,
and `repeated` cannot be written beside it.

- `whitespace`: cells are runs of non-space characters; at most
  `max_fields` (default: number of columns) cells are produced and the
  last one is the rest of the line, without the whitespace at its two
  ends and with every run inside it kept. A value with whitespace in it
  can therefore be read in the last cell and in no other.
- `aligned`: cells are cut where the header words start. A value that
  crosses a boundary from the right (a wide, right-aligned number) moves
  the cut to the previous space; a value that overflows to the right is
  kept whole when the rest of the row moved right with it. Empty cells
  are `null`. With explicit `columns`, extra trailing header words
  ("Mounted on") belong to the last column.

  Two rows are refused, because the header does not say where their
  cells are. One is a value that runs past where the next column starts
  and leaves that column empty: the column may have been empty, or its
  header word may be the second word of the name before it (`CONTAINER
  ID`). The other is a cell before the last that holds a tab or two
  spaces in a row, the gap that stands between columns: a right-aligned
  value with a space in it (`4min 27s`) starts before its header at a
  space, and the cut leaves part of it in the cell before. A value that
  itself holds two spaces (a date padded as `Sep  4`) can only be read
  in the last column, or by an expression.
  Positions are counted in the columns of a terminal, the way C tools
  and systemd line a table up: a CJK character or a kana takes two, a
  combining accent none. A tool that pads by counting characters instead
  (Go's `text/tabwriter`) lines up a row holding such characters
  differently, and that row is cut in the wrong place.
- `delimiter`: `strings.SplitN` on the literal, cells trimmed.
- `box`: a table drawn with rules, as MySQL, psql and `sqlite3` in box
  mode print one. The vertical bars say where the cells are, so nothing
  is counted or aligned and a value wider than its column cannot shift a
  boundary. A rule is a line with nothing on it but `+-=~`, a character
  from the Unicode Box Drawing block, and whitespace; the frame around
  the table and the rule under the header are both recognised without the
  definition describing either.

  The rules separate the header from the body. Inside the body a line is
  a row of its own, which is what those tools print, and a line whose
  first cell is empty continues the row above it, which is how a table
  that wraps a long value writes the rest of it. A header written over
  two lines is one name, joined with `_`; a value continued on the next
  line is one cell, joined with a newline. An empty cell is `null`.
  `max_fields`, `min_fields` and `header.none` do not apply.

  A row whose first column is genuinely blank cannot be told from a
  continuation, because in this format they are the same line. A table
  with such a column is one to read some other way. For the same reason
  a table with no rule under its header, several lines between its two
  frame rules, is refused: a header over two lines and a headless table
  of rows are the same text there.

  A line with neither a bar nor a rule on it has no cells, and a cell
  past the last column the header names has no name to go under; both
  are errors rather than text missing from the rows.

Missing trailing cells (at least `min_fields` present) are `null`.

### type: csv

```yaml
parse:
  type: csv
  delimiter: ","                             # default ","; "\t" for TSV
  header:
    columns: [name, size]                    # explicit names
    none: true                               # no header line
    rename: {qty: quantity}
```

Result: an array of objects, one per row. It is a table whose cells are
cut by a delimiter that a value may itself contain, which is what
separates it from `split: delimiter`: a value wrapped in `"` may hold the
delimiter, a line break, or a quote written twice (RFC 4180). The first
row names the columns unless the definition does, and the names are
normalised the way a table header is.

A row shorter than the header leaves the remaining keys `null`, so every
object of a document carries the same keys. A row longer than the header
is an error: a value with no column to go under has nowhere to be
reported. An error names the line of the input the record starts on,
whatever came before it.

A csv is data, so a row that holds the header's values is a row:
`header.repeated` defaults to false here, and a definition for a
command that prints its header again writes `repeated: true` to have
such a row start another table instead.

The records are made before anything else looks at the lines: a quoted
value may hold line breaks, and the lines it holds are part of the
record before they are lines. So `skip_blank` does not drop a blank
line inside a quoted value, `input.ignore` does not see inside one (an
expression is matched against the record, whose first line it opens
with), `input.fold` joins a continuation onto a record, and
`input.select` counts records. A csv part of a `composite` reads the
lines of its region as the top level left them, which have been through
`skip_blank` and `ignore` line by line.

With `header.none: true` every line is a record, the first one included.
The columns are the ones `columns` names, or, when it names none,
`column_1`, `column_2` and so on, as many as the first record has; a
later record with more fields is then the error above, and one with
fewer leaves nulls. The first record is what fixes the count because it
is the one thing a stream knows before the rest has arrived, so the
whole document and `--stream` agree. `--columns NAME,...` on the command
line gives such a definition its names without writing it out
(`csv/comma-no-header` and `csv/tab-no-header` are the registered ones). A header naming one column twice numbers the repeats
(`a`, `a_2`), because refusing a file a spreadsheet exported would be
the wrong answer and hiding one of the values would be worse. A number
is only given where no heading already has that name, so `x, x, x_2`
becomes `x`, `x_3`, `x_2` and every value keeps a key of its own; a
`rename` that lands on another heading is numbered the same way. The
delimiter is one character that is not a quote, a line break, a NUL
byte or invalid UTF-8, and one that is not is refused when the
definition is loaded.

### type: ini

```yaml
parse:
  type: ini
  separator: "="                             # default "="
  trim: true                                 # default true
  unquote: false                             # drop one surrounding pair of quotes
```

Result: an object of objects. A `[section]` heading opens an outer key
and every `key = value` line under it becomes an inner one, which is what
systemd units, git configuration and desktop entries are written in.
Keys written before the first heading go under the empty name, and there
is no such key when the file has no preamble. Lines starting with `#` or
`;` are comments; either character inside a value is part of the value,
since a password or a path may contain one. A section written twice
continues the first, and a key written twice in one section is an error
naming both lines: keeping the last value would lose the other, and an
array would mean a key's type depended on how many times it appeared.

Because the result is one object, an ini parser has no streaming form.

### type: tree

```yaml
parse:
  type: tree
  indent: "\t"                  # one level of indentation, as it is written
  # or the forms one level may take, tried in the order written:
  # indent: ["  ", "`-"]
  node:
    parse:
      type: regex               # or kv; a node is one line
      patterns:
        - '^(?P<slot>\S+) (?P<class>[^:]+): (?P<device>.+)$'
        - '^(?P<key>[^:]+): (?P<value>.*)$'
        - '^(?P<text>.+)$'
    fields:
      revision: {when_missing: omit}
```

Result: an array of nodes. Every line is a node; its depth is how many
levels of indentation open it, and its children are the lines under it.

`indent` is one level as it is written — `"\t"`, `"  "` — or a list of
the forms one level may take, for a report that marks a level with a
branch character. `systemd-analyze critical-chain` indents by two
characters that are two spaces or a backtick and a dash, `| ` and `|-`
where the chain branches, and the same drawn with box-drawing
characters in a UTF-8 locale, so it writes
`["  ", "`-", "| ", "|-", "│ ", "└─", "├─"]`; the first form that fits
at each step is the one taken, so the order is the order they are tried
in.
A node is the fields `node` reads from its line plus a `children` array,
which is `[]` when nothing follows it — so a consumer walks every node
the same way. A `node` pattern cannot name a group `children`.

This is the one parser whose result has a depth the definition does not
state. `lspci -vv` prints a device, its capabilities under it and a
capability's flags under those, and how far that goes is a property of
the machine rather than of the format. What the definition states is what
a node is; what the input states is nothing but how deep the nodes go.

`node.parse` is `regex` (with `pattern` or an ordered `patterns` list) or
`kv`, and the same description applies at every depth: the shapes that
appear at different depths are what the alternatives are for.

Three things the input may not decide:

- A line indented two levels below the one above it has no parent and is
  an error, rather than being attached to the nearest ancestor.
- Indentation that is not a whole number of `indent` is an error, rather
  than being rounded down and put under the wrong parent.
- Depth stops at 32.

A tree may be a `composite` part, which is what a report with a banner
above the tree needs, and a `records` part, which is what a report of
repeating blocks with a tree inside each needs (`sensors -u`). The two
decide different things and do not conflict: `start` says where a record
begins and `indent` says how deep a line inside one is.

With `--stream`, one top-level node is written per line, once nothing
deeper follows it.

### type: regex

```yaml
parse:
  type: regex
  pattern: '^(?P<filesystem>.+?) on (?P<mount_point>.+?) type (?P<type>\S+)$'
  each: line | input        # default line
```

Several alternatives can be listed instead of one expression, and are
tried in the order given; the first that matches decides how the line is
read. This is how a definition treats structurally different lines
differently without a single expression having to guess:

```yaml
parse:
  type: regex
  patterns:
    # only a mode starting with "l" makes " -> " a link separator
    - '^(?P<flags>l\S+)\s+(?P<filename>.+?) -> (?P<link_to>.+)$'
    - '^(?P<flags>\S+)\s+(?P<filename>.+)$'
fields:
  link_to: {when_missing: omit}
```

A group that only appears in some alternatives is simply absent from the
objects the others produce.

An alternative can also state values of its own, which is how a format
that mixes two kinds of line says which kind a record is:

```yaml
parse:
  type: regex
  patterns:
    - pattern: '^(?P<name>[^ =]+)=(?P<value>.*)$'
      values: {kind: environment}
    - pattern: '^(?P<minute>\S+) (?P<command>.+)$'
      values: {kind: job}
```

A stated value is a string key the alternative always gives, written
before its groups (in the order of their names when there are several),
and it goes through `fields` like any other value. It may not have the
name of a group of its own pattern. An alternative without values is
written as the expression alone, as in the example before this one.

`each: line` yields an array with one object per line built from the
named groups; `each: input` matches the whole (pre-processed) text once
and yields a single object. Groups that did not participate are `null`
(or omitted with `when_missing: omit`). A non-matching line is an error
naming the line and the pattern.

A match has to reach both ends of the line it reads; surrounding
whitespace aside, text before or after it is an error naming the line and
the column, since it is a value the definition never looked at. With
`each: input` the lines the match covers are the ones read: a line it
does not reach is unread, and so is the part of a line it starts or ends
inside. A line that belongs to another part of
a composite is named in that part's [`ignore`](#type-composite); a line
that belongs to nothing is a definition that does not describe its
input.

### type: kv

```yaml
parse:
  type: kv
  separator: "="          # default "="
  as: list | map          # default list
  trim: true              # default true
  unquote: false          # default false
```

A kv definition looks its `fields` entries up by the key the command
printed, not by a name the definition chose, so those entries are written
exactly as the key appears: `"CPU(s)"`, `"Thread(s) per core"`. Elsewhere
a field name has to be an identifier, because elsewhere it is a name the
definition picked.

`list` yields `[{"name": ..., "value": ...}]`; `map` yields one object,
and a key printed twice is an error naming both lines: an object holds one
value per key, so one of the two would be missing, and the same value
twice usually means two documents read as one. Keys and values are trimmed unless
`trim: false`, which a format whose values are significant down to the
space (an environment variable) sets. `unquote: true` removes one
matching pair of surrounding `"` or `'` from the value, for the
shell-quoted files (`/etc/os-release`) whose quotes are syntax rather
than content. `fields` entries are looked up by key, before any
conversion, so the key has to be a legal field name: a format whose
labels contain spaces or brackets (`CPU(s)`) cannot convert its values.

### type: records

```yaml
parse:
  type: records
  start: '^\d+: '        # a line matching this opens a record
  parts: [...]           # the same parts as composite
  # or, where a block is one value rather than named regions:
  record:
    parse: {type: kv, separator: ':', as: map}
    fields: {...}
```

A line matching `start` opens a record and everything up to the next such
line belongs to it, which is the shape of a report of repeating blocks:
an interface followed by its counters, a crate followed by its binaries.
The result is an array with one object per record.

How a block is read is said once, by `parts` or by `record`. With `parts`
the block is read the way `composite` reads a whole input, so the parts
are written once and applied to every block and each record is an object
keyed by part name. With `record` one parser reads the whole block and
the record is the object that parser yields, which is what a block that
is one labelled list (`stat` printing a file) or one expression has:
there are no regions to name, and a name invented for the only one would
be in every object of the result.

`record.parse` has to yield one object, so it is a `regex` with
`each: input`, a `kv` with `as: map`, or an `ini`. A parser that yields a
list is refused there: a record that is a list has nowhere to be, and
what the definition means is either a list of objects or a part with a
name to hold the list.

Text before the first record is an error naming the line, rather than
something quietly dropped. A banner belongs in `input.select`, or in a
`composite` part of its own with the blocks in a second part.

`records` is not recursive: a part of it cannot be `records` or
`composite`, and neither can `record.parse`. A record is a block that
repeats at one level, so its own depth is stated by the definition and
there is nothing for the input to say about it. A part may be a `tree`, which decides a different thing —
how deep a line inside one block is — and `sensors -u` needs both.

### type: composite

```yaml
parse:
  type: composite
  parts:
    - name: uptime
      select: {limit: 1}
      parse: {type: regex, each: input, pattern: '...'}
      fields: {...}
    - name: users
      select: {skip: 1}
      parse: {type: table, split: aligned}
```

Each part re-selects from the pre-processed lines and runs its own
parser; the result is an object keyed by part name.

A part may also carry `ignore`, a list of expressions matching lines of
its region that belong to a sibling part:

```yaml
parts:
  - name: settings
    select: {until: '^Boot[0-9A-Fa-f]{4}'}
    parse: {type: kv, separator: ':', as: map}
  - name: entries
    ignore: ['^[A-Za-z][A-Za-z0-9]*: ']
    parse: {type: regex, pattern: '^Boot(?P<boot_number>[0-9A-Fa-f]{4})...'}
```

`select` runs first and `ignore` narrows what it left, so `skip` and
`limit` count the lines as they stand in the output. Use it where two
parts share a region and neither can be cut out by a range: the settings
above a list of boot entries, the slave links between two labelled
blocks. What it is not for is silence. Every line of a part's region that
`ignore` does not name has to be read, and a line it does name has to be
read by the sibling it is handed to, so a line the definition never
anticipated is an error rather than a value quietly missing from the
JSON.

A part may be `records`, which is what a report that opens with a banner
and then repeats a block needs: one part reads the banner, the next reads
the blocks.

```yaml
parse:
  type: composite
  parts:
    - name: alternative
      select: {limit: 4}
      parse: {type: kv, separator: ':', as: map}
    - name: candidates
      select: {after: '^Alternative:'}
      parse:
        type: records
        start: '^Alternative:'
        parts: [...]
```

No other nesting is allowed. A part cannot be `composite`, and a part of
`records` cannot be `records`: both describe a depth that comes from the
input rather than from the definition, which is the shape jz does not
promise.

With `--stream`, a composite is one document per part as each part is
read: `{"part": NAME, "value": VALUE}`. A part whose parser yields a list
(a table, csv, a tree, a regex matched per line, a kv list, `records`) is
one document per element, written when the element is complete; any
other part is one document, written when its region has ended: at its
`until` line, when it has taken its `limit`, or at the end of the input.
The values of a list part, in order, are that part's list in the whole
document, and a part that is one value has exactly one document. A line
no part's region takes is reported where it is; a line that only
single-value parts took, and that none of them read, is reported once
they have all been read.

## fields

Every extracted value is a string (or `null` for an empty aligned cell /
non-participating group). `fields` maps a column, group or key name to a
conversion. It sits beside the parser that reads the values: at the top
level for a table, csv, regex, kv, ini or tree-less parser, under
`parts[]` for a `composite` or `records`, under `record` for a `records`
whose block is one value, and under `node` for a `tree`.
A `fields` map beside a composite, records or tree parser applies to
nothing and is refused when the definition is loaded, rather than left
out in silence; for a `records` written with `record` the rules go under
`record.fields`.

```yaml
fields:
  use_percent: {type: int, trim_suffix: "%", null_if: ["-"]}
  modified:    {type: time, layout: "2006-01-02 15:04:05 -0700"}
  login_at:    {type: time, layout: "Jan _2 15:04", year: assumed}
  elapsed:     {type: duration, layout: mm:ss}
  ro:          {type: bool, true_values: [1, yes], false_values: [0, no]}
  options:     {type: array, split: ","}
  groups:
    type: array
    split: ","
    items:
      type: object
      regex: '^(?P<id>\d+)\((?P<name>[^)]*)\)$'
      fields: {id: {type: int}}
  context:     {type: object, when_missing: omit, regex: '...'}
  mounted_on:  {required: true}
```

| Key | Applies to | Meaning |
|-----|-----------|---------|
| `type` | all | `string` (default), `int`, `float`, `bool`, `time`, `duration`, `array`, `object` |
| `trim_prefix`, `trim_suffix` | all | removed before conversion; without them a string value keeps its whitespace exactly as the parser produced it |
| `null_if` | all | values (after trimming) that become `null` |
| `required` | all | `null`/empty is an error |
| `when_missing` | all | `null` (default) or `omit` the key when the value is missing |
| `layout` | time | the [Go reference layout](https://pkg.go.dev/time#pkg-constants) the timestamp is written in; required, and it has to state a year unless `year: assumed` says the format prints none |
| `year` | time | `assumed` for a format that prints no year; the value stays a string until `--assume-year` says which year to read it in |
| `location` | time | how to read a timestamp that states no zone: `utc` (default) or `local`, the zone the running system is in |
| `layout` | duration | `h:mm` or `mm:ss`, saying what the last part of a bare `4:50` is; required |
| `true_values`, `false_values` | bool | spellings (case-insensitive); defaults are true/yes/on/1/y and false/no/off/0/n |
| `split`, `split_regex` | array | how to split; items are trimmed. A `split_regex` that matches the empty string is an error, since it would split between every character |
| `items` | array | conversion applied to each element (arrays of arrays are not allowed) |
| `regex` | string | the part of the value to keep: the one named group of the expression, which has to match the whole value. A value outside it is an error, and a group that takes no part leaves the value missing |
| `unescape` | string | escapes to undo after `regex`: `sequences` maps each escape as printed to the text it stands for, and every escape begins with the same character; any other escape is an error. `when` names a group of the pattern, and the escapes are undone only on a line where that group matched some text |
| `regex`, `fields` | object | named groups become keys; `fields` converts them; the match has to cover the whole value |

`regex` on a string field takes off what a command prints around a
value, such as the tree lsblk draws in front of a device name. The
expression names the one group that is kept:

```yaml
fields:
  name: {regex: '(?:(?:[|│] |  )*(?:[|`]-|[├└]─))?(?P<name>.+)'}
```

It is also how a definition refuses a value it cannot read one way. A
value outside the expression is an error, so an expression that leaves
out the ambiguous text turns it into a refusal instead of a guess.

`unescape` undoes an escaping that the format defines and the text
declares. GNU md5sum puts a backslash in front of the digest of a line
whose name it escaped, so `when` names the group that captures that
mark, and a line without it keeps its backslashes:

```yaml
parse:
  type: regex
  pattern: '^(?P<escaped>\\)?(?P<checksum>[0-9a-fA-F]{32}) [ *](?P<file>.+)$'
fields:
  file: {unescape: {when: escaped, sequences: {'\\': '\', '\n': "\n", '\r': "\r"}}}
```

An escaping the text does not declare, such as a quoting style an
option or a version chooses, is not one to decode: the same text then
names two different files. Such a value is refused with a `regex`.

There is no type that turns `955M` or `1.8T` into bytes. A size printed
with a unit is rounded to fit the column (`df -h`, `ls -lh`, `free -h`),
so the number of bytes it stands for depends on a base and a precision
the text does not state, and any integer jz wrote for it would be one
the command never printed. Such a value stays the string it was printed
as; a size printed as a plain count is an `int`. A value and its unit
printed as an exact pair (`MemTotal: 32790384 kB`) can be read into an
`object` with the two as separate keys.

`time` writes the value as an RFC 3339 string and never as an epoch
number, so a timestamp has one shape in the output and a consumer never
has to ask which of two a field carries. The same rule as for a rounded
size applies to what it is used on: a timestamp is converted only where
the text says what it means.

- No year, no conversion, unless the caller supplies one. `who`, `last`
  and `journalctl -o short` print `Sep  7 14:20`, and dating that means
  picking a year the output does not name. Such a field writes
  `year: assumed` beside a layout with no year, which says the format
  prints none; the value then stays the string it was printed as until
  `--assume-year 2025` or `--assume-year now` says which year to read it
  in. A layout without a year and without `year: assumed` is a
  validation error, so nothing is dated by accident.
- A zone abbreviation is not an offset, unless the caller supplies one.
  `JST` means +0900 only if you carry a table of abbreviations, which jz
  does not: the same three letters name different offsets in different
  parts of the world. A layout with `MST` in it converts when the
  abbreviation says its own offset (`UTC`, `GMT`) and otherwise waits for
  `--assume-zone JST=+0900`, which may be repeated. Until then `date`,
  `timedatectl` and `systemctl list-timers` keep those timestamps as
  text. A numeric offset in the text needs no table, which is what
  `journalctl -o short-iso`, `stat`, `mtr` and `date -R` print.
- `location` is for a timestamp with no zone at all, and it is a
  statement the definition makes rather than something jz works out. An
  archive listing (`tar -tv`, `unzip -l`) prints the local time of the
  machine that wrote the archive, which neither `utc` nor `local`
  describes, so those stay text too.

`duration` turns a printed length of time into seconds, as an integer
when the length is a whole number of them and a decimal when it is not.
It reads the spellings commands print:

```text
3-04:05:06     days-hours:minutes:seconds (ps -o etime)
04:05:06       hours:minutes:seconds
04:05          settled by layout: h:mm or mm:ss
01:23.45       the same with a fraction on the last part
13:42m         a trailing unit names the unit of the last part
13 days, 4:30  days and a clock reading (uptime)
45 min         a number and the unit it is in
3days
1h2m3s         units run together, largest first
3d4h
```

A unit spelled as a word (`min`, `Hours`) is read whatever its case. A
unit of one or two letters is read as written: `m` is a minute, and `M`,
which systemd writes for a month and a size writes for a megabyte, is
no unit a duration reads.

`layout` is required and is one of two words rather than a Go layout. It
says what the last part of a bare two-part reading is: `ps` prints four
minutes fifty seconds as `4:50` and `uptime` prints an hour and
twenty-three minutes as `1:23`, and nothing in the text separates them.
Being wrong about it is a factor of sixty that nothing downstream would
notice, so there is no default.

The rule about rounded values applies here as it does to sizes, and it
is about what the text names rather than how coarse it is.
`13 days, 4:30` names exactly 1139400 seconds, so it converts, even
though the machine has been up for some seconds more. `1.8T` names no
particular number of bytes until a base and a precision are chosen for
it, so it does not. What that rules out is a column that prints two
shapes of different precision: `top`'s TIME+ falls back from
`mmm:ss.hh` to `hhh,mm` for a long-running task, so the same field would
carry seconds for one row and minutes for another, and it stays text.

Nesting is limited to 8 levels.


## The registry manifest

`registry.yaml` sits at the top of a registry directory and describes the
registry rather than any one definition. It is optional: a directory
holding only `parsers/` is a registry that disables nothing.

```yaml
format: 1                 # required; the same schema version definitions carry
name: mine                # required; how the registry appears in diagnostics
version: "2026.09"        # optional
description: my parsers   # optional
source: https://...       # optional; where the registry came from
disable:                  # optional; definitions below this registry to switch off
  - file/posix            # one definition
  - du                    # every variant of a command
```

`disable` reaches downwards only: it applies to the registries below this
one in the layering, never to this one and never to one above it. A
disabled definition is not loaded at all, so it is absent from `jz list`,
from `--parser` and from automatic detection alike. This is what to reach
for when an official definition misreads your output: shadowing it means
writing a whole definition under the same name, while disabling it takes
it out and leaves the rest.

An entry that names no definition is a warning on standard error, not an
error, because a registry that disables a definition removed upstream
should keep working. An entry that is neither `command` nor
`command/variant` is a validation error and the registry does not load.
`jz list --sources` counts, per registry, the definitions a registry
above it switched off.

## Errors

Validation errors carry the source and a dotted path:

```
user:parsers/x/y/parser.yaml: parse.pattern: invalid regular expression: missing closing )
user:parsers/x/y/parser.yaml: fields.elapsed.layout: must be h:mm or mm:ss for type duration, not "hh:mm"
```

Parse errors carry the definition, line number and field:

```
df/gnu: line 3: field "used": cannot convert "abc" to int: invalid syntax
mount/linux: line 7: line does not match pattern /^(?P<filesystem>.+?) on .../: "garbage"
```

Text the definition did not read is named where it is, the first few
lines of it quoted and the rest counted; text a pattern stopped short of
on a line it read is named with its column. This is two `dig` replies in
one capture, of which the definition reads one:

```
dig/bind: line 21: 11 lines no part of the definition read: line 21 "; <<>> DiG 9.20.24-1ubuntu0.3-Ubuntu <<>> nonexistent-host.invalid", line 22 ";; global options: +cmd", line 23 ";; Got answer:" and 8 more
```

## Limits

| Limit | Value |
|-------|-------|
| definition file | 256 KiB |
| regular expression | 2048 characters |
| columns | 256 |
| composite parts | 32 |
| regex alternatives | 16 |
| field nesting | 8 |
| signature window | 200 lines |
| input, and a command's stdout | 64 MiB (internal) |
| what `--stream` holds while a record waits for its end | 64 MiB |
| line | 1 MiB |
