---
title: Write a parser
description: How to add a parser to jsonize with YAML and a fixture, and how to contribute it.
toc: true
---

This walkthrough uses the existing `lsof/linux` definition as an example.
The same steps apply to a new command or variant. Writing a parser needs
YAML and captured output; contributing to this repository also needs Go
to run its checks.

## 0. Look at what is already there

```console
$ ls registry/parsers/lsof
$ jz list lsof
```

A command with a definition already has a directory of variants, and
what you are adding is likely a variant beside them: another
implementation, or an option that changes the columns. `jz list` names
every command the registry reads, and `jz list <command>` names its
variants. For a contribution, choose a format that is not already
covered. To try this example without editing the official definition,
use the personal registry described at the end of this guide.

The format may already be read under another command's name: GNU
`sha256sum --tag` prints the line FreeBSD's `md5` prints, and `podman ps`
prints what `docker ps` does. Give jz a capture before writing anything:

```console
$ jz --explain --file capture.txt
```

If a definition chooses it, or is listed as close, and reads the same
lines, the command belongs in that definition's `aliases` rather than in
a definition of its own (definition-format.md, aliases). Two definitions
that read the same text fail the checks in step 5, because nothing in
the text says which of them is right.

## 1. Capture output

```console
$ LC_ALL=C lsof -p $$ > lsof.txt
```

Capture from the real implementation you are describing and record what
it was (`lsof 4.95 on Ubuntu 24.04`). If the command prints
machine-specific values (host names, users), replace them consistently;
do not change the layout. Formats you cannot run yourself (another OS)
need a fixture whose origin you can state; CI additionally runs the real
command on Linux and macOS runners (`e2e/atago/exec_*.atago.yaml`), and
a command that needs a device the runner lacks is run there on its
captured output through the same path. An entry there is welcome when
the command is on the runner, and not required: the fixtures are the
contract, and the checks below run on them.

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

For `lsof`, `SIZE/OFF` may be blank and `NAME` may contain spaces.
Split at the header's column positions to keep those cells in place:

```yaml
format: 1
command: lsof
variant: linux
description: lsof default columns
metadata:
  compatible: [lsof]
  references: [https://github.com/lsof-org/lsof]
detect:
  os: [linux]
  signature:
    all: ['^COMMAND\s+PID\s+USER\s+FD\s+TYPE\s+DEVICE\s+SIZE/OFF\s+NODE\s+NAME\s*$']
parse:
  type: table
  split: aligned
fields:
  command: {required: true}
  pid: {type: int, required: true}
  user: {required: true}
  fd: {required: true}
  name: {required: true}
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
is not, say what every line of it looks like. Where the format has a
first line of its own, state it with an expression starting with `\A`;
[Writing a signature](../definition-format/#writing-a-signature) says
why. A signature is also where
you state what this definition does not undertake to read; that is a
scope you choose, and the comment beside the expression is where you
say it.

Two facts about what the expressions see. The signature reads the first
twenty lines (`window`), joined with line breaks and with no break after
the last, so `\z` is the end of those lines, which is the end of the text
only when it is that short: "every line looks like this" written with
`\A` and `\z` holds for a long output too. And `detect.args` looks for a
word anywhere among the arguments, not at a position, so
`any: [madison]` is also met by `apt-cache policy madison`; the signature
is what refuses that output.

What not to reach for is `none`. If a neighbouring format matches your
signature, the signature is not yet saying what your format is, and
excluding the neighbour couples your definition to theirs: they change
it, your definition breaks, and the list never stops growing.
[The reference](../definition-format/#writing-a-signature) has the
detail.

## 4. Add fixtures

```
registry/parsers/lsof/linux/testdata/lsof-4.95.txt
registry/parsers/lsof/linux/testdata/lsof-4.95.yaml
```

A case is found by its `.txt`; the `.yaml` and `.json` of the same name
go with it, and `jz test` fails on one that has no `.txt` beside it, as
a case that would never run.

Fixtures verify the signature you decided on; they do not decide it.
Capture one per thing the definition claims, including the shapes it
claims to refuse, so that the file beside the definition says what the
definition means rather than what happened to pass.

The base name of the `.txt` says what the case is (`interval.txt`,
`one-snap.txt`) or which implementation the text came from
(`lsof-4.95.txt`), whichever tells the next reader why the case is there.

The `.yaml` file documents the capture and feeds the selection test:

```yaml
description: one process named on the command line, with a deleted file open
source: captured on Ubuntu 24.04 (lsof 4.95) with LC_ALL=C; user names replaced
os: linux
args: [-p, "1234"]
```

`description` is optional and says in one line which of the definition's
claims this case is there for.

`source` is required, and what belongs in it is how the text came to
exist: the command line, the implementation and its version, the
operating system, the locale, and what you replaced afterwards. Say which
of three things it is, because a reader cannot tell from the text and the
three mean different things. Output *captured* from a running command is
evidence that the definition describes what the command does. Text
*written by hand* exercises a rule the definition states and is evidence
only that the definition does what it says, which is what an
`expect_error` case and a boundary usually are. Text *quoted* from a
document is how a format you cannot run is described at all, and the
document belongs in `metadata.references` as well.

`args` is everything after the command name, a subcommand included
(`args: [madison, bash]` for `apt-cache madison bash`): it is what
`jz run` would see.

To pin text the definition must refuse, add a fixture with
`expect_error: <substring>` and no `.json` (a `.json` there would never
be compared, so `jz test` refuses it). It covers both ways a
definition refuses: the signature not describing the text, and the parse
failing on it. The substring is matched against whichever message comes
out, so `expect_error: 'does not describe this input'` pins a signature
that keeps a neighbouring format away. Such a fixture is usually another
format's output, so the check that no other definition reads your
fixtures leaves it out; `os` and `args` are only needed when the refusal
depends on them.

## 5. Generate the golden file and review it

```console
$ jz test --update ./registry
$ git diff --stat registry/parsers/lsof
$ cat registry/parsers/lsof/linux/testdata/lsof-4.95.json
```

In the jsonize repository itself, `make registry-update-golden` does the
first step and `make registry-test` the check below. The repository's
check also wants the definition's schema (step 6) and its row in the
parsers page (step 9), and says so by name until both are there; the
golden files are written either way.

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
$ jz list --schema lsof linux
```

The schema is derived from the definition: its keys, the types the
fields convert to, which keys every object carries and which values can
be null. Read it the way a consumer would. A key you meant to be always
there that shows as optional, or a number that shows as a string, is the
definition saying something other than what you meant. In the official
registry `make registry-update-schema` writes it to
`registry/schemas/lsof/linux.json`, where it is published. Run it when
a definition is added or its output changes; a change that only adds a
fixture leaves the schema as it was.

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

A definition can come to read a decoy rightly, when the decoy is output of
the format the definition was widened to cover. Move the file into that
definition's `testdata/` with a `.yaml` saying where it came from and
generate its golden file, rather than deleting it: the text stays pinned,
now as what must be read.

```console
$ cat > registry/testdata/decoys/my-lookalike.txt
$ jz test ./registry
```

## 8. Try it for real

```console
$ go run ./cmd/jz run lsof -p $$
$ lsof -p $$ | go run ./cmd/jz --pretty
$ go run ./cmd/jz --parser lsof --variant linux --file lsof.txt
```

A definition is named by its command and its variant as two options;
`lsof/linux` is how messages write it, not an argument jz takes.

## 9. Open the pull request

Include the definition, the fixtures, the golden JSON, the schema, any
decoy you added and the variant in the table in
`website/content/parsers.md`. Link each variant to its schema and keep
the variants in alphabetical order. Use a `parser:` commit prefix.

## Working outside the repository

The same loop works with an installed `jz` and a local registry. On
Linux with `lsof` installed, save the definition above as
`my-registry/parsers/lsof/linux/parser.yaml`, then capture a fixture:

```sh
mkdir -p my-registry/parsers/lsof/linux/testdata
# Save the definition above as my-registry/parsers/lsof/linux/parser.yaml.
LC_ALL=C lsof -p $$ > my-registry/parsers/lsof/linux/testdata/process.txt
cat > my-registry/parsers/lsof/linux/testdata/process.yaml <<EOF
source: captured locally with LC_ALL=C lsof -p $$ on Linux
os: linux
args: [-p, "$$"]
EOF
jz test --update ./my-registry
# Review my-registry/parsers/lsof/linux/testdata/process.json.
jz test ./my-registry
JSONIZE_REGISTRY_PATH="$PWD/my-registry" jz list lsof linux
JSONIZE_REGISTRY_PATH="$PWD/my-registry" jz --file my-registry/parsers/lsof/linux/testdata/process.txt
```

Record your lsof and OS versions in the fixture's `source` before sharing
it. The official fixtures are inside the binary, so the local definition
is checked against them without a copy of the repository.

## Reading the result from a script

`jz test --json` writes the same facts as one object on standard output,
for a job that has to count them rather than read them. The lines on
standard error and the exit status are the same with it as without.

```console
$ jz test --json ./my-registry
{
  "passed": 12,
  "failed": 1,
  "failures": [
    {
      "definition": "lsof/linux",
      "case": "process",
      "path": "parsers/lsof/linux/testdata/process.txt",
      "source": "./my-registry",
      "kind": "golden",
      "message": "output differs from process.json (-want +got):\n..."
    }
  ]
}
```

`kind` says what the case failed at, so a job can tell one kind of
failure from another without matching the message:

| Kind | What it means |
|------|---------------|
| `load` | the definition or its fixtures could not be read; the entry names the file rather than a definition |
| `select` | the wrong definition was chosen, none was, or another definition read a text that is not its own |
| `parse` | the fixture could not be read with its own definition |
| `refuse` | a case that states the text is refused was read, or was refused for another reason |
| `stream` | `--stream` reads the text differently from the way the whole document is read |
| `unread` | a changed copy of the text did not change the answer, which is text the definition never read |
| `schema` | the output does not fit the schema its definition derives |
| `golden` | the output differs from the JSON beside the fixture, or there is none |

`JSONIZE_REGISTRY_PATH` makes the local definition shadow the official
one of the same command and variant. For a persistent installation, move
the definition into the [user registry](../usage/#registries).
