---
title: Write a parser
description: How to add a parser to jsonize with YAML and a fixture, and how to contribute it.
toc: true
---

This is the walkthrough for a pull request that adds `lsof` support, but
the same steps apply to any command. Nothing here requires Go.

## 1. Capture output

```console
$ LC_ALL=C lsof -p $$ | head -20 > lsof.txt
```

Capture from the real implementation you are describing and record what
it was (`lsof 4.95 on Ubuntu 24.04`). If the command prints
machine-specific values (host names, users), replace them consistently;
do not change the layout. Formats you cannot run yourself (another OS)
need a fixture whose origin you can state; CI additionally runs the real
command on Linux and macOS runners (`e2e/atago/exec_*.atago.yaml`), and
a command that needs a device the runner lacks is run there on its
captured output through the same path.

## 2. Decide the variant

Look at what differs between implementations and options. Each output
*format* is one variant:

- same columns everywhere → one variant (`posix`, `default`)
- Linux vs macOS headers differ → `linux` / `bsd` (or `darwin`)
- an option changes the columns → `gnu` / `gnu-human`

Name the directory `registry/parsers/lsof/<variant>/`.

## 3. Write the definition

Start from the closest existing definition:

| Shape | Example |
|-------|---------|
| header + rows, last column may contain spaces | `registry/parsers/ps/unix/parser.yaml` |
| header + rows with empty cells | `registry/parsers/lsblk/linux/parser.yaml` |
| row label in the first column | `registry/parsers/free/gnu/parser.yaml` |
| one regex per line | `registry/parsers/mount/linux/parser.yaml` |
| one object from the whole output | `registry/parsers/uptime/linux/parser.yaml` |
| nested values | `registry/parsers/id/posix/parser.yaml` |
| summary line + table | `registry/parsers/w/linux/parser.yaml` |
| a block per subject, repeated | `registry/parsers/ip/stats-link/parser.yaml` |
| a header block, then repeated blocks | `registry/parsers/update-alternatives/query/parser.yaml` |

For `lsof`, the header is `COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME`
and `NAME` may contain spaces, so a whitespace table with explicit columns
fits:

```yaml
format: 1
command: lsof
variant: default
description: lsof default columns
metadata:
  compatible: [lsof]
  references: [https://github.com/lsof-org/lsof]
detect:
  signature:
    all: ['^COMMAND\s+PID\s+USER\s+FD\s+TYPE\s+DEVICE\s+SIZE/OFF\s+NODE\s+NAME\s*$']
parse:
  type: table
  header:
    columns: [command, pid, user, fd, type, device, size_off, node, name]
  min_fields: 8
fields:
  pid: {type: int}
```

Always add a `detect.signature`, even for a single variant: it is what
lets jz reject unrelated text with a clear message instead of producing
nonsense. If the shape is too generic to be evidence on its own (a number
and a path, three numbers in a row), add `auto_detect: false` next to it:
the definition is then used only when the parser is named, and its
signature is still checked there.

Write the signature as what must be true of the output, and decide it
before you look at whether the fixtures pass. Anchor the whole text with
`\A` and `\z` where the format is short enough to say so, and where it
is not, say what every line of it looks like. A signature is also where
you state what this definition does not undertake to read; that is a
scope you choose, and the comment beside the expression is where you
say it.

What not to reach for is `none`. If a neighbouring format matches your
signature, the signature is not yet saying what your format is, and
excluding the neighbour couples your definition to theirs: they change
it, your definition breaks, and the list never stops growing.
[The reference](/jsonize/definition-format/#writing-a-signature) has the
detail.

## 4. Add fixtures

```
registry/parsers/lsof/default/testdata/lsof-4.95.txt
registry/parsers/lsof/default/testdata/lsof-4.95.yaml
```

Fixtures verify the signature you decided on; they do not decide it.
Capture one per thing the definition claims, including the shapes it
claims to refuse, so that the file beside the definition says what the
definition means rather than what happened to pass.

The `.yaml` file documents the capture and feeds the selection test:

```yaml
source: captured on Ubuntu 24.04 (lsof 4.95) with LC_ALL=C; user names replaced
os: linux
args: [-p, "1234"]
```

To pin text the definition must refuse, add a fixture with
`expect_error: <substring>` and no `.json`. It covers both ways a
definition refuses: the signature not describing the text, and the parse
failing on it. The substring is matched against whichever message comes
out, so `expect_error: 'does not describe this input'` pins a signature
that keeps a neighbouring format away.

## 5. Generate the golden file and review it

```console
$ jz test --update ./registry
$ git diff --stat registry/parsers/lsof
$ cat registry/parsers/lsof/default/testdata/lsof-4.95.json
```

In the jsonize repository itself, `make registry-update-golden` does the
first step and `make registry-test` the check below.

Check types, `null`s and the last column. A rounded, human-readable
number stays a string: the output does not record the base and the value
is already rounded, so converting it would invent precision. Then:

```console
$ jz test ./registry
```

The check fails if the fixture does not select its own variant, does not
parse, differs from the JSON, or if any definition reads a fixture that
belongs to another one. That last check is the one that catches a
signature written wide enough to swallow a neighbouring format. When it
is your fixture that another definition reads, that definition's
signature says less than its format does: sharpen it to state what its
own output has, rather than listing yours under its `none`, and never
drop or trim your fixture to get past it.

It also fails if the definition leaves text out without saying so. Every
line of the fixture has to be read, blank, or named by an `ignore`
expression, and a pattern has to reach both ends of the line it reads.
Then the fixture is changed the ways real input goes wrong — a foreign
line at the end, the same line in the middle, the whole fixture twice —
and a definition that still succeeds has to carry the foreign line in
its result. A list read from the fixture twice has to be the records of
one copy twice, no fewer and nothing in between: a heading matched only
at the start (`\A` in `select.after`) lets the second copy's heading in
as a record. An `ignore` that names more than the definition means to
drop is what this catches.

The opposite changes have to change nothing. The fixture with CRLF line
endings, with a byte order mark, without its last line break, and with a
blank line before or after it is the same text, and it has to pick the
same definition and give the same JSON. jz takes care of all but one: a
blank line after the text is yours only if the definition keeps blank
lines as lines to read (`skip_blank: false`).

## 6. Look at the schema of what it produces

```console
$ jz list --schema lsof default
```

The schema is derived from the definition: its keys, the types the
fields convert to, which keys every object carries and which values can
be null. Read it the way a consumer would. A key you meant to be always
there that shows as optional, or a number that shows as a string, is the
definition saying something other than what you meant. In the official
registry `make registry-update-schema` writes it to
`registry/schemas/lsof/default.json`, where it is published.

## 7. Check it against the decoys

`registry/testdata/decoys/` holds text that belongs to no format jz
reads: prose, a build log, a CHANGELOG, an INI file, a shell script, a
document that quotes a report's header line. Every definition is applied
to every one of them, and reading one is a failure.

Every file in there was read by some definition once. A signature that
looks for a single recognisable line will find it in a document that
merely quotes that line, and a parser that takes any word where the
format has a vocabulary will turn a sentence into a record. The corpus is
how those two mistakes stay fixed.

If your format resembles something ordinary, add a file. There is nothing
to register: the check reads the directory.

```console
$ cat > registry/testdata/decoys/my-lookalike.txt
$ jz test ./registry
```

## 8. Try it for real

```console
$ go run ./cmd/jz run lsof -p $$
$ lsof -p $$ | go run ./cmd/jz --pretty
```

## 9. Open the pull request

Include the definition, the fixtures, the golden JSON, the schema, any
decoy you added and the variant in the table in
`website/content/parsers.md`, linked to its schema. Use a
`parser:` commit prefix.

## Working outside the repository

The same loop works with a personal registry and the released binary:

```console
$ mkdir -p ~/.config/jsonize/registry/parsers/lsof/default/testdata
$ $EDITOR ~/.config/jsonize/registry/parsers/lsof/default/parser.yaml
$ jz test --update
$ jz test
```

With no directory, `jz test` checks the registries jz would use and
leaves the built-in one alone. The official fixtures are inside the
binary either way, so your definition is held to the same exclusivity
check as an official one without a copy of the repository.

`jz list` shows the definition with source `user`, and it shadows an
official definition of the same command and variant.
