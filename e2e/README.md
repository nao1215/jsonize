# End-to-end tests

The suite under `e2e/atago` runs the built `jz` binary the way a user
does and pins the command line contract: exit codes, standard error,
the JSON shape, and which definition real output lands on. It is run by
[atago](https://github.com/nao1215/atago) on Linux, macOS, Windows and
FreeBSD in CI (`.github/workflows/e2e.yml`) and locally with `make e2e`.
FreeBSD has no hosted runner, so it runs in a virtual machine on the
Linux one.

## What it guarantees

| Spec | Runs on | What it pins |
|------|---------|--------------|
| `detect` | all | identification from a pipe or a file, `--extract`, `--raw`, `--explain` and `--explain=json`, the path of a file naming its definition; ping and stat saved on macOS and FreeBSD, HTTP/2 and HTTP/3 headers, `ls/names` read only when named, a csv without a header line; `kubectl get pods`, `go test`, `gzip -l`, `eza -l`, `cargo tree` and `systemd-cgls` with padded process ids |
| `errors` | all | every refusal: unidentified, ambiguous, mismatched, oversized input, text the definition did not read, removed options, another command's output |
| `misdetect` | all | real output of one command is not read by another definition, with or without `--parser` |
| `registry` | all | `jz list` (with `--schema`), `jz test`, user and `JSONIZE_REGISTRY_PATH` registries, layering, broken definitions |
| `report` | all | sysstat reports read as an array of samples, from captured output |
| `adhoc` | all | `--define`, and the shape definitions (csv, ini, drawn table) |
| `cookbook` | all, `jz run` not on Windows | every recipe of the site's cookbook, one scenario named after each section; `TestCookbookRecipesAreRun` fails when a recipe has no scenario |
| `new` | all | `jz new`: strings, JSON values, arrays with `[]`, files and standard input as values, a data file read by its extension, `--string` with special characters and Unicode, `--text-file` line endings, `--path` nesting and its conflicts, `--each` per record with late failures, and the arguments refused before anything is read |
| `datafile` | all | data files read as the format their extension names (csv, tsv, ltsv, JSON Lines, JSON, YAML, gzip and bzip2), piped data read as `--format` names, `text`, `lines` and `nul` with their boundaries and streamed records, `--type` conversions and refusals, the line a reading stops at, and the option pairs that are refused |
| `stream` | all | `--stream` writes one document per record and keeps what was written when a later record fails; a composite part by part; every stream ending at a bad record |
| `docs` | all, one macOS | every jz command the README, the landing page and the usage guide show with its output, run with the input that prints it; `TestDocumentedExamplesAreRun` fails when a page shows one no scenario runs |
| `http` | all, HTTP/2 not on Windows | `jz run curl -I` and `-D -` against `e2ehelper serve` on 127.0.0.1: redirects, interim responses, a proxy's CONNECT, repeated headers |
| `completion` | all, zsh not on Windows | the bash and zsh scripts loaded into the real shell: subcommands, options, parsers of every registry, variants, paths, nothing run |
| `process` | all, part POSIX | what `jz run` does when the reader leaves, when it is interrupted, when `--timeout` passes: no hang, no process left behind, the status the shell sees; Ctrl-C typed at a terminal (a pty) reaching the command once |
| `exec` | POSIX, git on all | `jz run` with a shell script standing in for the command: status mirroring, signals, `--timeout`, argument boundary; and `jz run git log --oneline` in a repository whose configuration decorates the log, on every system |
| `exec_linux` | Linux | the host's real coreutils, procps, util-linux, iproute2, sysstat, systemd and BusyBox, on files, processes and sockets the scenarios make themselves; hardware listings on captured output |
| `exec_darwin` | macOS | the real BSD commands land on the `bsd` definitions and the GNU shape is refused, ping, ping6 and stat included |
| `exec_freebsd` | FreeBSD | the real commands land where a FreeBSD machine showed they do: `ps aux` and `mount` on the `bsd` definitions, `id`, `du` and `wc` on the POSIX ones, and the summary line macOS shares with no other BSD refused rather than cut |
| `exec_windows` | Windows | the real `ipconfig`, `ipconfig /all` and `systeminfo` land on their definitions, the neighbouring variant is refused, and the host name, the build number and an adapter's address and MAC agree with `hostname`, `ver` and PowerShell |
| `hardware_linux` | Linux, optional | the same hardware listings on a real device, where there is one |

Windows runs every spec: a scenario that belongs to one operating system
says so with `only:` or `skip:`, so a spec added later is on Windows
from the day it exists. Besides `exec_windows`, what Windows verifies is
the command line contract and the pipe, file and `--stream` paths, plus
`jz run` with the suite's own helper as the command. CI runs it on
windows-latest (Server 2025) and on windows-2022, both English with the
console on code page 65001. Output in another display language, or in a
code page that is not UTF-8, is not run anywhere.

A value the machine decides is never pinned as a constant. Where a
scenario needs a known process, file or socket, it makes one: a `sleep`
it starts for `pidstat -p`, a file it writes for `ls -lG`, a listening
and a connected socket for `ss`. Each is a service or a file of the
scenario and goes away with it. Where the value can only be the
machine's, it is compared with a second source on the same machine
(`stat`, `hostname`, PowerShell) rather than with what one runner
printed once.

## Required commands

Nothing in a required spec is gated on a command being present: a
missing one fails the scenario. CI installs on Linux what the image
lacks:

| Command | Package (Debian/Ubuntu) | Used by |
|---------|-------------------------|---------|
| `iostat`, `mpstat`, `pidstat`, `vmstat` | `sysstat`, `procps` | `exec_linux` |
| `busybox` | `busybox` | `exec_linux` |
| `zsh` | `zsh` | `completion` (macOS ships it; Windows has none, and those scenarios say so) |
| `bash`, `curl` | on every runner, Windows through Git | `completion`, `http`, `cookbook`, and bash for the connected socket in `exec_linux` |
| `jq`, `sed` | on every runner, Windows through Git; the FreeBSD machine installs jq | `cookbook`: the recipes that hand the JSON to jq are pipelines, and the pipeline is what they pin |
| `git` | on every runner; the FreeBSD machine installs it | `exec`, `exec_linux` |
| `df`, `free`, `ps`, `uptime`, `id`, `env`, `mount`, `du`, `stat`, `lscpu`, `lsmod`, `prlimit`, `who`, `wc`, `ls`, `ln`, `md5sum`, `sha256sum`, `tar`, `gzip` | on every runner | `exec_linux` |
| `ip`, `ss`, `dpkg`, `apt-cache` | on every runner | `exec_linux` |
| a running systemd | the runner boots with it; a container usually does not | `exec_linux` (`systemctl`) |
| `ipconfig`, `systeminfo`, `hostname`, `powershell` | part of Windows | `exec_windows` |
| `go` | the toolchain | `scripts/run_e2e.sh` builds `jz` and `e2ehelper` |

`scripts/run_e2e.sh` checks for these before the run and names what is
missing, before the report and again under a failed one, so a scenario
that failed for want of a command is not mistaken for a bug in jz.

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

`e2e/atago/e2ehelper` is a small Go program `scripts/run_e2e.sh` builds
before the run. `emit` is a command that keeps printing (a burst, then a
line at a time), `pipe` is a reader that takes a few lines and leaves,
then reports what became of `jz` and of the producer, and `gone` waits
for a process to end. It is there so the scenarios about a reader that
leaves or a command that is stopped run on Windows too, where there is
no shell script to stand in for either. `serve` is the local HTTP
server the `curl` scenarios read and whose listening socket `ss` lists,
and `as` makes the stand-ins `jz run` is pointed at.
