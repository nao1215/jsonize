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
      --extract KEY   keep only this key (repeatable)
      --exclude KEY   drop this key (repeatable)
      --parser NAME   restrict detection to one parser
      --variant NAME  use a variant of --parser
  -h, --help          show help
```

`jz run` adds `--env NAME=VALUE`, `--keep-locale` and `--timeout`, which
control the command rather than the conversion. `jz list` adds `--json`
and `--sources`.

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
