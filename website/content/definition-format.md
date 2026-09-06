---
title: Definition format
description: "The YAML a parser definition is written in: detection, parsing and field conversion."
toc: true
---

A parser definition is one YAML document, `parser.yaml`, stored at
`parsers/<command>/<variant>/` inside a registry. The directory names
must match the `command` and `variant` keys.

```yaml
format: 1                       # required; the schema version
command: df                     # required; [a-z0-9][a-z0-9._+-]*
variant: gnu-human              # required; [a-z0-9][a-z0-9-]*
description: GNU coreutils df -h
metadata: {...}                 # optional, informational
detect: {...}                   # optional; how to choose this variant
exec: {...}                     # optional; environment for jz run
input: {...}                    # optional; line pre-processing
parse: {...}                    # required; the algorithm
fields: {...}                   # optional; conversions per field
```

Unknown keys are errors. Names such as `aux`, `con` or `nul` are rejected
because they cannot be directories on Windows.

## metadata

| Key | Meaning |
|-----|---------|
| `tags` | free-form strings shown by `jz list --json` |
| `references` | URLs (manuals, source) |
| `authors` | who maintains the definition |
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
- `args` applies only in exec mode. Bundled short flags are expanded, so
  `-hT` satisfies `any: ["-h"]`.
- `signature` expressions are matched against the first `window` lines
  joined with newlines, in multi-line mode (`^`/`$` match line
  boundaries). `\A` anchors at the start of the input.
- `auto_detect: false` marks a format whose text is not evidence on its
  own: three numbers in a row, or a number and a path, describe far too
  many things. Such a definition is skipped by automatic detection and
  used only when the parser is named (`--parser du`, or `jz run du`),
  where its signature is still checked. Prefer it to a growing list of
  `none` expressions excluding every other format that looks similar.

Selection: a candidate whose applicable criterion fails is rejected. One
remaining candidate is the answer. Several are settled by `priority` only
when they are variants of the same command and one priority is strictly
highest; otherwise the selection is an error naming them.

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
  ignore: ['^total \d']    # drop matching lines
  skip_blank: true          # default true
  select:                  # applied in this order
    after: '^BEGIN'        # drop up to and including the first match
    until: '^END'          # stop before the first match
    skip: 1                # drop N leading lines
    limit: 50              # keep at most N lines
```

Input is split on the record separator, a newline by default; a trailing
`\r` is then removed from every line and a UTF-8 BOM is dropped. With
`record_separator: nul` the records are separated by NUL bytes instead,
which is what makes a value containing a newline representable. Input
must be valid UTF-8 and within the size limits.

## parse

### type: table

```yaml
parse:
  type: table
  split: whitespace | aligned | delimiter   # default whitespace
  delimiter: "\t"                            # with split: delimiter
  header:
    columns: [filesystem, size, used]        # explicit names
    none: true                               # no header line (columns required)
    leading_label: type                      # name for an unlabelled first column
    rename: {login: login_at}                # rename derived names
  max_fields: 6                              # whitespace/delimiter: last cell absorbs the rest
  min_fields: 3                              # rows with fewer cells are errors (default: column count)
```

Result: an array of objects, one per row, keys in column order. Without
`header.columns` the names are derived from the header: lower-cased,
non-alphanumerics become `_`, a leading or trailing `%` becomes
`_percent` (`%CPU` → `cpu_percent`, `Use%` → `use_percent`,
`1K-blocks` → `1k_blocks`, `Mounted on` → `mounted_on`).

- `whitespace`: cells are runs of non-space characters; at most
  `max_fields` (default: number of columns) cells are produced and the
  last one keeps the rest of the line verbatim.
- `aligned`: cells are cut where the header words start. A value that
  crosses a boundary from the right (a wide, right-aligned number) moves
  the cut to the previous space; a value that overflows to the right is
  kept whole. Empty cells are `null`. With explicit `columns`, extra
  trailing header words ("Mounted on") belong to the last column.
- `delimiter`: `strings.SplitN` on the literal, cells trimmed.

Missing trailing cells (at least `min_fields` present) are `null`.

### type: regex

```yaml
parse:
  type: regex
  pattern: '^(?P<filesystem>.+?) on (?P<mount_point>.+?) type (?P<type>\S+)$'
  each: line | input        # default line
  on_mismatch: error | skip # default error
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

`each: line` yields an array with one object per line built from the
named groups; `each: input` matches the whole (pre-processed) text once
and yields a single object. Groups that did not participate are `null`
(or omitted with `when_missing: omit`). A non-matching line is an error
naming the line and pattern unless `on_mismatch: skip`.

### type: kv

```yaml
parse:
  type: kv
  separator: "="          # default "="
  as: list | map          # default list
  trim: true              # default true
  unquote: false          # default false
  on_mismatch: error | skip
```

A kv definition looks its `fields` entries up by the key the command
printed, not by a name the definition chose, so those entries are written
exactly as the key appears: `"CPU(s)"`, `"Thread(s) per core"`. Elsewhere
a field name has to be an identifier, because elsewhere it is a name the
definition picked.

`list` yields `[{"name": ..., "value": ...}]`; `map` yields one object
(later duplicates win). Keys and values are trimmed unless
`trim: false`, which a format whose values are significant down to the
space (an environment variable) sets. `unquote: true` removes one
matching pair of surrounding `"` or `'` from the value, for the
shell-quoted files (`/etc/os-release`) whose quotes are syntax rather
than content. `fields` entries are looked up by key, before any
conversion, so the key has to be a legal field name: a format whose
labels contain spaces or brackets (`CPU(s)`) cannot convert its values.

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
parser; the result is an object keyed by part name. Parts cannot be
composite themselves.

## fields

Every extracted value is a string (or `null` for an empty aligned cell /
non-participating group). `fields` maps a column, group or key name to a
conversion:

```yaml
fields:
  use_percent: {type: int, trim_suffix: "%", null_if: ["-"]}
  size:        {type: size, unit: binary}
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
| `type` | all | `string` (default), `int`, `float`, `bool`, `size`, `array`, `object` |
| `trim_prefix`, `trim_suffix` | all | removed before conversion; without them a string value keeps its whitespace exactly as the parser produced it |
| `null_if` | all | values (after trimming) that become `null` |
| `required` | all | `null`/empty is an error |
| `when_missing` | all | `null` (default) or `omit` the key when the value is missing |
| `unit` | size | `binary` (K=1024, default) or `decimal` (K=1000); `Ki`/`Mi` always mean 1024 |
| `true_values`, `false_values` | bool | spellings (case-insensitive); defaults are true/yes/on/1/y and false/no/off/0/n |
| `split`, `split_regex` | array | how to split; items are trimmed |
| `items` | array | conversion applied to each element (arrays of arrays are not allowed) |
| `regex`, `fields` | object | named groups become keys; `fields` converts them |

`size` accepts `1024`, `955M`, `3.7G`, `466Gi`, `1.2 MiB`, `0B` and
returns bytes as an integer (rounded). Use it only where the base is
certain and the value is not already rounded: a human-readable size
printed by `df -h` or `ls -lh` is neither, and the official definitions
keep such values as the strings they were printed as. Nesting is limited
to 8 levels.


## Errors

Validation errors carry the source and a dotted path:

```
user:parsers/x/y/parser.yaml: parse.pattern: invalid regular expression: missing closing )
user:parsers/x/y/parser.yaml: fields.size.unit: must be binary or decimal
```

Parse errors carry the definition, line number and field:

```
df/gnu: line 3: field "used": cannot convert "abc" to int: invalid syntax
mount/linux: line 7: line does not match pattern /^(?P<filesystem>.+?) on .../: "garbage"
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
| line | 1 MiB |
