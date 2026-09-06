# Adding a parser to the official registry

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
command on Linux and macOS runners (`e2e/atago/exec_*.atago.yaml`).

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
lets `jz parse` reject unrelated text with a clear message instead of
producing nonsense.

## 4. Add fixtures

```
registry/parsers/lsof/default/testdata/lsof-4.95.txt
registry/parsers/lsof/default/testdata/lsof-4.95.yaml
```

The `.yaml` file documents the capture and feeds the selection test:

```yaml
source: captured on Ubuntu 24.04 (lsof 4.95) with LC_ALL=C; user names replaced
os: linux
args: [-p, "1234"]
```

To pin an error path, add a fixture with `expect_error: <substring>` and
no `.json`.

## 5. Generate the golden file and review it

```console
$ make golden-update
$ git diff --stat registry/parsers/lsof
$ cat registry/parsers/lsof/default/testdata/lsof-4.95.json
```

Check types, `null`s and the last column. Then:

```console
$ make test
```

The golden test fails if the fixture does not select its own variant
(signature overlap with another variant), does not parse, or differs
from the JSON.

## 6. Try it for real

```console
$ go run ./cmd/jz run lsof -p $$
$ lsof -p $$ | go run ./cmd/jz parse --pretty lsof
```

## 7. Open the pull request

Include the definition, the fixtures, the golden JSON and one line in the
README table. Use a `parser:` commit prefix.

## Working outside the repository

The same loop works with a personal registry and the released binary:

```console
$ mkdir -p ~/.config/jsonize/registry/parsers/lsof/default/testdata
$ $EDITOR ~/.config/jsonize/registry/parsers/lsof/default/parser.yaml
$ jz validate --update ~/.config/jsonize/registry
$ jz validate ~/.config/jsonize/registry
```

`jz list` shows the definition with source `user`, and it shadows an
official definition of the same command and variant.
