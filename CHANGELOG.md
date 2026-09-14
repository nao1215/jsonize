# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `split: aligned` reads a heading of several words, such as
  `CONTAINER ID` or `Soft Limit`, as one column when `header.columns`
  gives it the name those words derive together.
- Definitions for `lshw -short` and `lshw -businfo`.
- Definitions for FreeBSD `md5` and the other BSD digest commands,
  `sysctl`, `vmstat -i`, `df -hT` and `arp -a`, and for OpenZFS
  `zfs list` and `zpool list`.
- Releases sign `checksums.txt` with cosign, include an SPDX SBOM for
  each archive, and publish a Homebrew cask to `nao1215/homebrew-tap`.
  A tag is published only after the release smoke checks pass on it.

### Fixed

- `sysctl/freebsd` no longer reads the lines of a value printed over
  several lines as variables of their own (`sysctl hw.intrs` returned a
  variable named `irq1`), and reads a name part holding a space, as the
  ZFS kstat histograms print.
- Eleven definitions no longer read a line of another command's output
  printed after their own. `ip link` took an unindented line as a
  setting of the last interface, `tc -s qdisc` as a statistic,
  `bridge fdb` as an entry, `systemd-analyze blame`, `getconf -a`,
  `lsattr`, `loginctl list-seats`, `systemd-id128 show` and
  `git status --porcelain` as rows, and `unzip -l` and `modinfo`
  dropped such a line through an ignore rule that matched only its
  start. Each now refuses it.
- `modinfo` reads a parameter description written over several lines
  (`modinfo drm`), which lost every line after the first, and a driver
  that names more than nineteen firmware files (`modinfo r8169`), which
  was not recognised.

### Changed

- `scc/default` reports the byte count as the string scc printed, as it
  does every other count: scc 4.1 prints it with thousands separators,
  which was refused. Its output schema is version 2.

## [0.1.0]

The first release of jsonize and its `jz` command.

### Added

- `jz` converts command output from standard input or `--file` into
  JSON. It identifies the format from the text, and refuses input it
  cannot identify or read completely instead of guessing.
- `jz run COMMAND [args...]` runs a command without a shell, converts its
  standard output and exits with the command's status. The command name
  and its arguments narrow the choice of definition; `--env`,
  `--keep-locale` and `--timeout` control how the command runs.
- Output options: `--pretty`, `--yaml`, `--stream` for commands that keep
  printing, `--extract` and `--exclude` for keys, and `--raw` for values
  without field conversion.
- Selection options: `--parser` and `--variant` name a definition,
  `--define` supplies one inline, `--columns` names csv columns, and
  `--explain` reports why a definition was chosen.
- `--assume-year` and `--assume-zone` for timestamps printed without a
  year or with a zone abbreviation.
- An embedded registry of 451 YAML definitions for 169 commands and
  files, covering GNU, BusyBox, macOS, FreeBSD and Windows variants where
  their output differs, with 1,226 captured or documented fixtures.
- A published JSON Schema for the output of every definition under
  `registry/schemas`, shown by `jz list --schema COMMAND VARIANT`.
- `jz list` for supported commands and definitions, and `jz test` for
  checking local definitions against their fixtures and the official
  fixture corpus.
- Layered registries: `JSONIZE_REGISTRY_PATH`, the user registry and the
  embedded registry. Definitions are data and are never downloaded.
- bash and zsh completion from `jz completion`.
- Go packages under `pkg/` for loading registries, selecting a definition
  and parsing text.
- Release archives for Linux, macOS and Windows on amd64 and arm64,
  `.deb`, `.rpm` and `.apk` packages, SHA-256 checksums and GitHub build
  provenance.

### Known limitations

- Rounded human-readable sizes such as `1.8T` stay strings.
- Some generic formats, such as `du`, `wc` and `env`, are read only when
  the parser is named.
- Windows command output is read for `ipconfig`, `ipconfig /all` and
  `systeminfo` in English. Other languages and code pages are not
  checked.
- FreeBSD is tested by the end-to-end suite but has no release archive.
  Install it with `go install`.
- Windows release binaries are built with `GOEXPERIMENT=nogreenteagc`,
  the garbage collector configuration the Windows CI jobs run.
