# End-to-end tests

The suite under `e2e/atago` runs the built `jz` binary the way a user
does and pins the command line contract: exit codes, standard error,
the JSON shape, and which definition real output lands on. It is run by
[atago](https://github.com/nao1215/atago) on Linux, macOS and Windows
in CI (`.github/workflows/e2e.yml`) and locally with `make e2e`.

## What it guarantees

| Spec | Runs on | What it pins |
|------|---------|--------------|
| `detect` | all | identification from a pipe or a file, `--extract`, `--raw`, `--explain`, the path of a file naming its definition |
| `errors` | all | every refusal: unidentified, ambiguous, mismatched, oversized input, removed options, another command's output |
| `misdetect` | all | real output of one command is not read by another definition, with or without `--parser` |
| `registry` | all | `jz list`, `jz test`, user and `JSONIZE_REGISTRY_PATH` registries, layering, broken definitions |
| `report` | all | sysstat reports read as an array of samples, from captured output |
| `adhoc` | all | `--define`, and the shape definitions (csv, ini, drawn table) |
| `stream` | all | `--stream` writes one document per record and keeps what was written when a later record fails |
| `process` | all, part POSIX | what `jz run` does when the reader leaves, when it is interrupted, when `--timeout` passes: no hang, no process left behind, the status the shell sees |
| `exec` | POSIX | `jz run` with a shell script standing in for the command: status mirroring, signals, `--timeout`, argument boundary |
| `exec_linux` | Linux | the host's real coreutils, procps, util-linux, iproute2, sysstat and BusyBox; hardware listings on captured output |
| `exec_darwin` | macOS | the real BSD commands land on the `bsd` definitions and the GNU shape is refused |
| `hardware_linux` | Linux, optional | the same hardware listings on a real device, where there is one |

Windows runs every spec: a scenario that belongs to one operating system
says so with `only:` or `skip:`, so a spec added later is on Windows
from the day it exists. Windows has no parser definitions of its own,
so what it verifies is the command line contract and the pipe, file and
`--stream` paths, plus `jz run` with the suite's own helper as the
command.

## Required commands

Nothing in a required spec is gated on a command being present: a
missing one fails the scenario. CI installs on Linux what the image
lacks:

| Command | Package (Debian/Ubuntu) | Used by |
|---------|-------------------------|---------|
| `iostat`, `mpstat`, `vmstat` | `sysstat`, `procps` | `exec_linux` |
| `busybox` | `busybox` | `exec_linux` |
| `df`, `free`, `ps`, `uptime`, `id`, `env`, `mount`, `du`, `stat`, `lscpu`, `lsmod`, `who`, `wc` | on every runner | `exec_linux` |
| `ip`, `dpkg`, `apt-cache` | on every runner | `exec_linux` |
| `go` | the toolchain | `process` builds `e2ehelper` |

## Optional: real hardware

`hardware_linux` runs `lspci -vv`, `iw dev`, `systemd-analyze`,
`blkid -o export` and `aplay -l` on the host's devices. Each scenario
is gated with `only: {command: ...}` on the *output* existing, not on
the binary: a machine with `aplay` and no sound card skips, and the
reason is in the log. The definitions those commands land on are
required reading all the same: `exec_linux` runs each command name with
a stand-in on PATH that prints output captured from the real one, so a
parser bug cannot hide behind a skip.

## Skips

`scripts/run_e2e.sh` reports in TAP so every skipped scenario is listed
with its reason, and it fails the run when a scenario was skipped for
any reason other than the operating system, unless its suite is named
`(optional)`. A skip in a required spec means a dependency is missing:
install it, or gate the scenario on `os`.

```console
$ make e2e                       # every spec, TAP, skip check
$ E2E_REPORT=console make e2e    # atago's progress output while writing a scenario
$ scripts/run_e2e.sh --filter 'BusyBox'   # any atago run flag passes through
```

## The helper

`e2e/atago/e2ehelper` is a small Go program the `process` suite builds
in its setup. `emit` is a command that keeps printing (a burst, then a
line at a time), `pipe` is a reader that takes a few lines and leaves,
then reports what became of `jz` and of the producer, and `gone` waits
for a process to end. It is there so the scenarios about a reader that
leaves or a command that is stopped run on Windows too, where there is
no shell script to stand in for either.
