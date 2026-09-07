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
  -f, --file PATH     read input from PATH instead of stdin
  -p, --pretty        indent JSON output
      --stream        write one record per line as it is read
      --extract KEY   keep only this key (repeatable)
      --exclude KEY   drop this key (repeatable)
      --parser NAME   restrict detection to one parser
      --variant NAME  use a variant of --parser
  -h, --help          show help
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
is a complete JSON document. A record that cannot be read still stops the
conversion with a message on standard error and exit status 3, but the
records already written stay written — they are finished documents, and
jz cannot take them back once they have left. That is the whole of the
difference, and it is the reason `--stream` is an option rather than the
default.

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

Two things are checked. Every `testdata/<case>.txt` is parsed with its own
definition and compared with the `.json` beside it, and every definition
is then named explicitly on every other definition's fixtures and must
refuse them. The official fixtures travel inside the binary, so the
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
| 3 | the input did not match the chosen definition |
| 4 | the format could not be identified, several matched, or a named one did not fit |
| 5 | a registry could not be loaded |
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
