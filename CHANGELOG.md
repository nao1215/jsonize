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
- Definitions for FreeBSD `kldstat -h`, `netstat -ib`, `netstat -r`,
  `sockstat -s`, `last -y` and `iostat -x`, and for `ldd` output that
  names each program above its libraries (glibc given several programs,
  FreeBSD always).
- With `--stream` and `--explain=json`, a record the stream leaves out is
  reported when it is left out, on its own `jz: explain: ` line of
  standard error: `{"event":"skipped","definition":...,"line":...,
  "reason":...,"skipped":N}`, where `skipped` counts the records left out
  so far. `--explain` writes the same fact as a line of text.
- Releases sign `checksums.txt` with cosign, include an SPDX SBOM for
  each archive, and publish a Homebrew cask to `nao1215/homebrew-tap`.
  A tag is published only after the release smoke checks pass on it.

### Fixed

- `ip address` output whose first `inet` line is past the first twenty
  lines, as when nine or more interfaces without an address come first,
  was read as `ip link` with each address and lifetime as a setting.
  iproute2's output is now read as `ip address` (its header has the
  group and no mode), and BusyBox's, whose header is the same for both,
  is refused.
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

- jz has no dependencies outside the Go standard library. The YAML of
  definitions, manifests, fixture metadata and `--define` is read by
  jz's own reader, which covers the part of YAML those files use (block
  and flow collections, plain, quoted and block scalars, comments, one
  document per file) and refuses anchors, aliases, tags and keys that
  are not one scalar, naming the line. A value that does not fit its key
  (`limit: abc`, `columns: x`) is now refused with its line and path.
  Loading the registry takes a third of what it did: `df -h | jz` 48 ms
  to 21 ms on the machine below.
- jz starts faster. The definitions of a registry are decoded side by
  side, and the sorted views a selection asks for are made once when the
  registry is loaded rather than on every lookup. On a 32-thread machine
  `df -h | jz` took 137 ms and takes 48 ms; `jz version`, which loads no
  registry, takes 1 ms either way.
- Detection over the whole registry joins the signature window once
  rather than once per definition, and no longer renders a reason for a
  definition whose first expression did not match, which `--explain`
  never showed. A scan of 1000 definitions takes half the time and 14
  allocations where it took 4000.
- The engine and the JSON writer allocate less: the records of an input
  are pieces of one copy of it, a table row is cut into cells without
  boxing each of them twice, an object keeps an index of its members only
  past eight of them, and strings and numbers are written without an
  encoder per value. A table of 100 000 rows is read in 59 ms where it
  took 109 ms, and its JSON is written in 19 ms where it took 73 ms (26
  ms where it took 125 ms indented). The output is byte for byte what it
  was, and the jsonutil tests now pin it against encoding/json.
- `--stream` commits to a definition as soon as the lines that have come
  can no longer change the choice, instead of holding back the widest
  signature window of every candidate. `jz run --stream vmstat 1` wrote
  its first record after about 17 seconds and now writes it after the
  header and the first sample; piped with automatic detection it chose
  after 200 lines and now after 2, writing the first record on the third.
  A definition whose signature a later line could still meet or rule out
  keeps the stream waiting, up to its window, unless a definition that
  already fits would win over it.
- 83 more definitions state what their text opens with (`\A` in
  `signature.all`), and `vmstat/linux` and `vmstat/linux-active` now also
  anchor their second line, which is what lets a stream rule them out on
  its first lines. Text with a line before the one a definition opens with
  (a warning above the `df` header that is not a `df:` message) is now
  refused as unidentified, exit 4, where it was chosen and failed to
  parse, exit 3.
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
