# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- A tree definition can state what its top-level line looks like with
  `parse.root`. The node alternatives read every depth, and the one
  that reads the continuation of a wrapped list reads any text, so a
  word printed after `lspci -v`, or another command's output piped in
  behind it, was read as one more device with no children; with `root`
  it is refused, naming the line. `lspci -v`, `lsusb -v`, `lsusb -t`,
  `iw dev`, `apt-cache depends`, `systemctl list-dependencies` and
  `systemd-analyze critical-chain` state theirs.
- 61 definitions for 25 more commands, 187 commands and 528 definitions
  in all. Archive listings: `gzip -l` and `-lv`, `xz -l` and `--robot
  -l`, `zstd -l` and `-lv`, `lz4 --list`, `7z l` and `7z l -slt`.
  Checks and keys: `md5sum -c` and the other digest commands' `-c`,
  `ssh-keyscan`, `ssh-add -L` (and `ssh-add -l` as `ssh-keygen -l`),
  `openssl x509 -noout` with `-subject`, `-issuer`, `-dates`, `-serial`
  and `-fingerprint`, `passwd -S`, `screen -ls`. System: `snap services`,
  `snap connections` and `snap changes`, `lsipc -q`, `-m` and `-s`,
  `nmcli radio`, `timedatectl show`, `loginctl show-user`, `show-session`
  and `show-seat`, `apt-cache search`, `dpkg-query -W`, `pip freeze` and
  `pip list --outdated`. Developer tooling: `go test` result lines and
  `go test -bench` reports, `gh pr list`, `issue list`, `run list`,
  `release list`, `workflow list` and `repo list`, `rustup check`,
  `cargo search`, `just --list`, `uv python list`, `mise outdated`.
  Containers and clusters: `docker system df` and `system df -v`,
  `docker context ls`, `docker compose ls` and `compose ps`,
  `docker ps -s`, `docker version`, `kubectl get pods`, `nodes`,
  `services`, `deployments` and `namespaces` in their default, wide and
  all-namespaces forms, `kubectl config get-contexts`,
  `kubectl api-resources`, `kubectl version`, `rclone lsl`, `lsd` and
  `version`, `redis-cli info` and `client list`.
- `unescape` takes `octal: true`, which reads a backslash and three
  octal digits as the byte they name, and `quote`, which decodes only a
  value put between that character. git and getfacl write names that
  way.
- A definition for `systemd-cgls`: the control groups under the group,
  unit or directory it starts from, the processes in each with their
  ids, whether a group is delegated, and the group ids and extended
  attributes systemd 252 prints to root. In a `tree`, a form of
  indentation that ends in a branch character (`|-`, `└─`) ends the
  indentation, so the blanks after it that pad a right-aligned number
  belong to the node.
- Definitions for `eza -l` (with `--header`, `-a`, `-B`, and in its
  long-iso, full-iso, `-g` and `-H` column sets), `procs`, `tokei`,
  `bat --list-languages` and `hyperfine --style basic`.
- Definitions for `cargo tree`, `uv tree`, `npm ls --all`, `busctl tree`
  (one service or several), `docker buildx ls` and `xinput list`.
- `md5sum --tag`, `sha1sum --tag`, `sha256sum --tag`, `b2sum --tag` and
  the rest of the GNU and uutils family are read by the definition of
  the BSD digest line, which they print, together with FreeBSD and macOS
  `md5`. A name the tools escape is decoded and marked `escaped`.
- When `jz run` has no parser for a command and nothing in its arguments
  names one, the refusal shows how to name one:
  `jz run --parser PARSER -- COMMAND ...`.
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

- Ctrl-C typed at a terminal reached the command `jz run` started twice:
  once from the terminal, which sends it to the whole foreground group,
  and once passed on by jz. A command that stops at once on a second
  interrupt was stopped by one keypress. jz no longer passes on an
  interrupt when the terminal's foreground group is its own and the
  command is in it.
- `jz run ls -l` for a user whose environment sets `QUOTING_STYLE`
  returned names with the quotes in them. jz runs ls with the literal
  style, and the long listings refuse `-Q`, `--quoting-style`, `-b` and
  `-q`, which change the names they print.
- `jz run git log --oneline` in a repository or for a user whose
  configuration sets `log.decorate` returned the branch names as part of
  the first subject. jz runs git with decorations and signatures off for
  this format, refuses `-c` among its arguments, and on a pipe refuses a
  line whose parenthesis opens with `HEAD`, a tag or a remote branch.
  `exec.env` now comes from the variants the arguments leave, so the
  setting does not reach `git config --list`.
- A duration with a fraction of a unit under a second is the number the
  text names: `1.3 ms` is `0.0013` where it was `0.0013000000000000002`,
  and `systemd-analyze critical-chain` reports `+87ms` as `0.087`. The
  parts of a duration are added up exactly and turned into a number
  once.
- `jz run systeminfo /fo csv` no longer suggests that systeminfo runs
  csv. A name whose definitions only describe a shape (`csv`, `table`)
  is an option's value when it appears among a command's arguments.
- `jz run systemctl list-units --plain` failed with exit 3 while a unit
  had a job pending. `--plain` drops the marker column, and the
  definition for the table with a JOB column expected the header to be
  indented for it. That table now has its own definition,
  `systemctl/units-jobs-plain`, and `systemctl/units-jobs` reads only
  the indented header.
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

- A definition's regular expressions are checked when it is loaded and
  built into programs the first time they are matched, so a run no
  longer builds the programs of the hundreds of definitions it does not
  read with. Loading the official registry allocates half of what it
  did, `df -h | jz` on one core takes 42 ms where it took 53 ms, and a
  broken expression is still reported when the definition is loaded.
- `git status --porcelain` paths are the names they stand for: a path git
  quoted has its quotes removed and its escapes decoded, octal bytes to
  UTF-8 (`"docs/\346\227\245..."` is `docs/日...`). `getfacl` decodes
  the backslash and the octal escapes it writes in a file name.
- `--assume-year now` dates a timestamp printed without a year in the
  latest year that does not put it after the moment jz started, rather
  than always in the current year. A `last` or `who` line of December 31
  read in January used to come out in the future.
- `jz test` and `make registry-test` report a definition that reads a
  fixture or a decoy once, naming every name that reached it
  (`md5/bsd (also named sha256sum, b2sum)`), where a definition with
  fourteen names used to be reported fourteen times.
- Fourteen table definitions state the shape of the cells that tell a
  row from prose: a version opens with a digit (`pip list`, `mise ls`,
  `npm outdated`), an address is one (`/etc/hosts`), a unit file ends
  in its type (`systemctl list-unit-files`), a row of `free` is `Mem`,
  `Swap`, `Total`, `Low` or `High`, a git configuration name has a dot,
  a docker identifier is hex, a scope is one of three words, a volume
  name holds no space, and an alternative's status is `auto` or
  `manual` with an absolute path. A shell prompt or a word echoed after
  the command's output used to be read as one more row and is now
  refused with the line named. Joined to the tree change above, the
  pairs of one command's fixture followed by another's that a
  definition read as its own fell from 19,582 to 9,512 of 1.1 million
  tried; what is left is the `Label: value` shape and two-word tables,
  where no cell has a shape to hold a line to.
- The Go toolchain the module asks for is 1.26.6, so a build with an
  older 1.26 fetches it: govulncheck names four standard library
  fixes between 1.26.4 and 1.26.6 that reach jz's code.
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
