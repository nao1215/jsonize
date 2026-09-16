# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Seventeen definitions. Eight commands jz did not read: `apt-config
  dump`, `avahi-browse -p` (and with `-r` the resolved host, address,
  port and TXT record; a browse left running is read as a stream),
  `fc-match`, `gio mime TYPE`, `gpgconf --list-dirs` and
  `--list-components`, `gsettings list-recursively` (a value kept as the
  GVariant text printed), `pwdx` (read only when named) and
  `systemd-delta --diff=false`. More of three it did: `git diff
  --name-status` (a quoted path decoded), `git reflog`, `git ls-remote`,
  `git cherry`, `systemd-analyze timespan`, `timestamp` and `calendar`,
  and `resolvectl dns` and `domain`.
- Three more: `sum` (the BSD checksum, told from cksum by its padding),
  `ipcs -l` (a record per resource) and `df -P` with `-B`, `-m` or
  `--block-size` (the unit the heading names in bytes). The registry
  holds 236 commands through 620 definitions.
- `sar -d`, the activity of each block device, and for ELF files
  `readelf -S -W` (the section headers), `readelf -d` (the dynamic
  section) and `size -A` (the size and address of each section), and
  `rustc -vV` and `cargo -vV`. The registry holds 237 commands through
  626 definitions.
- `jz run --format NAME` reads the command's output as a data format,
  the way `COMMAND | jz --format NAME` reads it, with `--stream`,
  `--columns` and `--type` as there. `jz run --type` already told the
  caller to give `--format csv`, which `jz run` did not take.

### Changed

- `jz new KEY=@FILE` and `--text-file KEY=PATH` leave out a byte order
  mark at the start of the file, as `--format text` and `jz new --each
  KEY=@-` already did. A document built from a file saved with one no
  longer starts its value with U+FEFF.

### Fixed

- A command that prints at an interval gets its records out while it
  runs. `jz run --stream ping HOST` never chose a definition, since
  ping/linux asked for the summary ping prints only at its end, and
  `jz run ping -c 25 HOST` failed with exit 4 because that summary came
  past the lines a signature looks at. `free -h -s 1` and `journalctl
  -f` waited for twenty lines or for the command to end before their
  first record, since a rival variant could still have matched a later
  line; the free and journalctl signatures now look at the lines those
  formats open with. The same summary rule is gone from FreeBSD
  `vmstat -i`, whose interrupt list can be longer than the window.
- `docker stats` without `--no-stream` is read: each refresh leaves a
  space before the clear-to-end-of-line sequence, which failed the row.
- An `int` field, and `--type COLUMN=int`, keeps an integer past 64 bits
  with every digit instead of refusing it with exit 3: `ipcs -l` prints
  a shared memory limit of 2^64 - 4. A `+` and zeros in front are left
  out as they are for a smaller integer.
- `jz run` refuses, before it starts the command, `--stream` when no
  variant it could read the output with has a streaming form, and a key
  to `--extract` or `--exclude` that none of them can have. It used to
  run the command first, and with `--define` or no output at all it
  either wrote `[]` for the key or stopped the command part way.
- A key the definition cannot have is exit 2 whether the input is read
  whole or as a stream; read whole, a line the definition could not read
  was reported first, with 3.
- A command `jz run` started that fails having printed nothing but blank
  lines gets no JSON and its own status, with `--define` and with
  `--stream` as without them: `--define` wrote `[]`, and `--stream`
  added a message about the empty output. Blank output from a command
  that succeeds is answered the same way with `--stream` as without.
- `--stream --extract KEY` on a csv or a tsv refuses a key no column has
  before writing a record, as the whole document does; it wrote an empty
  object per record and then refused the key.
- `jz run` returns the command's status when it failed and its output
  lacks a key named to `--extract`, as it does for every other failure
  to read the output; without `--stream` it returned 2.

## [0.6.0]

A stream ends at the first record it cannot read, so the next program in
a pipeline never sees a partial result that looks complete, and
`--stop-on-error` goes away with the choice it offered. `jz new KEY:=JSON`
returns the same status as the same text read from a file, and the
registry reads what exiftool, identify and pkg-config print: 227 commands
through 600 definitions.

### Added

- Four definitions for what three tools print: `exiftool FILE` (the tags
  one per line) and `exiftool -G FILE` (the group in front of each of
  them), `identify FILE...` (one record per image, its width and height
  typed), and `pkg-config --list-all` (the modules it knows, read only
  when named since a name and a phrase is also what a glossary looks
  like). The registry holds 227 commands through 600 definitions.

### Fixed

- `jz new KEY:=JSON` returns 3 rather than 2 when the argument is JSON
  and jz refuses what it holds: a key given twice in one object, or
  nesting past the depth limit. The same text in a file (`KEY:=@f.json`)
  and the same text read as data (`jz --format json`) already returned 3,
  so a script that built the argument with a command substitution got a
  different status from one that passed the file. Text that is not JSON
  at all is still 2, since that is the command line being wrong.

### Changed

- A stream ends at the first record it cannot read, rather than leaving
  the record out and going on. `--stream`, `jz run --stream` and `jz new
  --each` all do this, for a command's output, a data file and `lines` or
  `nul` alike: the records written before the failure stand, it is
  reported once on standard error, nothing after it is read, and the
  status is 3. A stream that goes on past a record it dropped reaches the
  next program in the pipeline looking complete, since the diagnostic
  went to standard error and the status comes only after the records have
  been read and acted on.

### Removed

- `--stop-on-error` asked for what every stream now does. A command line
  that still names it is refused with exit status 2 and a message saying
  so; drop the option and the behaviour is unchanged.
- `engine.Stream` no longer takes an `onError func(*ParseError) error`
  beside `emit`: with one answer to a record that cannot be read there is
  nothing to choose. A caller passing `nil` today drops the argument and
  gets the same reading.

## [0.5.0]

jz holds every reading of an input to the same rules. A stream is
identified under the record limit it is read under and drops a file path
that does not fit, the way a whole document does; a JSON escape that
names no character and a control character standing in YAML text are
refused rather than replaced; YAML nests as deeply as JSON. A table can
ask for its rows to be counted instead of letting the last column take
the rest of the line, `jz test --json` writes what failed as one object,
and loading the registry costs a tenth less time and a fifth fewer
allocations. The registry is unchanged at 224 commands and 596
definitions.

### Added

- `jz test --json` writes the result to standard output as one object:
  the counts, and a failure per entry with the definition, the case, the
  fixture path, the registry it came from, what it failed at and the
  message. The kinds are load, select, parse, refuse, stream, unread,
  schema and golden, so a job can tell one failure from another without
  matching the message. The lines on standard error and the exit status
  are the same with the option as without.

### Changed

- `max_fields` of a `table` may be one more than the number of columns,
  which is how a definition asks for a row to be counted rather than for
  its last column to take the rest of the line. `ps aux` keeps the
  default; `proc/diskstats` now counts every row, where a twenty-first
  field used to be caught only by failing to convert to an integer in
  the last column.
- Loading the registry costs about a tenth less time, a tenth fewer
  bytes and a fifth fewer allocations. The walk that looks for
  definitions no longer descends into the fixtures beside them (5,614
  files were read through to find 596), a definition file is read into
  one buffer of its own length, and the YAML reader takes its nodes from
  blocks, leaves text with no CRLF in it where it lies and does not
  build a scalar written on one line. Nothing about what is chosen, what
  is reported or which registry wins changes.

### Fixed

- The examples that append to an array are quoted, in the README, the
  usage guide, the cookbook, `jz new --help` and the demo recording.
  `jz new tags[]=web` copied into zsh, which is what macOS starts with,
  was refused by the shell with `no matches found` before jz saw it.
- `--stream` holds the lines it identifies the format by to the 1 MiB
  record limit that the reading of a record already applied, so a
  producer that never ends a record is refused at the byte past the
  limit with exit 3 instead of waited for. Two megabytes with no line
  break were refused at once with `--define` and waited for with
  `--parser df --variant gnu`.
- `--stream --file` drops a file path that named a definition the text
  does not fit, the way the whole-document reading does, and reads the
  text on its own terms. A df report saved as `/etc/fstab` was exit 4
  with `--stream` and exit 0 without it.
- Input that cannot be read at all, such as a gzip file whose checksum
  does not match, ends `--stream` with the status the whole-document
  reading ends with: exit 3 rather than exit 1.
- A `\u` escape naming half of a surrogate pair in JSON or JSON Lines is
  refused with the line it is on, where it was read as U+FFFD and
  written out with exit 0: `{"name":"\ud800"}` became `{"name":"\ufffd"}`.
  A pair written in full and a replacement character the text holds for
  itself are unchanged.
- A control character standing in YAML text is refused, as YAML 1.2.2
  does not allow it there. `key: ab<NUL>cd` was read as a value holding
  it. One written as an escape (`"a\0b"`) is still a character of the
  value.
- YAML nests as deeply as JSON: both are held to 1000 levels of arrays
  and objects, where YAML stopped at 100 and counted a scalar as a
  level.
- JSON, JSON Lines, YAML, LTSV, `lines` and `nul` input, read with
  `--format`, a file's extension or `jz new key:=@file`, is held to the
  4,194,304 values a csv and a definition's reading were already held to:
  a JSON array of 4,194,305 numbers was read with exit 0. Past the limit
  a whole reading is exit 3 with nothing written. A stream counts each
  record, and a record past the limit is a record that cannot be read,
  with the records before it kept.
- `jz new` counts the values of the whole document it makes, so files each
  under the limit cannot make one past it. `--each` counts each document
  and treats one past the limit as a line that cannot be read.
- `jz new` reports a file or standard input past the 64 MiB limit with
  exit 3, as `--file` does, where it was exit 1.
- Without `--extract` or `--exclude`, output is no longer copied object by
  object, and no option remembers every key it has seen: a stream of JSON
  Lines whose records each have keys of their own grew from 23 MiB to
  162 MiB of memory between 100,000 and 1,000,000 records, and now stays
  at 12 MiB with or without the options. A refusal of an unknown key
  names at most 64 of the keys the input had.

## [0.4.0]

jz is the step that makes JSON for the next command. `jz new` places
strings, file text and JSON values at keys or JSON Pointers, and makes one
document per line of standard input with `--each`; plain text reads as
strings with `--format text`, `lines` and `nul`; a csv or tsv column
takes a type with `--type`. `--yaml` output is removed. The registry is
unchanged at 224 commands and 596 definitions.

### Added

- `jz new --each` makes one document per line of standard input and
  writes each as a line of JSON when its line has come, holding nothing
  else of the input: `vmstat 1 | jz --stream | jz new --each host=a
  sample:=@-`. `KEY:=@-` is the line read as JSON, `KEY=@-` the line as a
  string, and the files the other arguments name are read once. A line
  that cannot be read is reported and left out, with the status 3.
- `--stop-on-error` ends a stream (`--stream`, `jz run --stream`, `jz new
  --each`) at the first record it cannot read, keeping the records written
  before it, with the status 3; a command `jz run` started is stopped.
- `--format text`, `--format lines` and `--format nul` read input that
  has no format of its own as strings: the whole input as one string, a
  list with one string per line (LF or CRLF), or one per NUL-terminated
  record as `find -print0` writes them. Nothing is trimmed, empty records
  stay `""`, and a separator at the end does not add an empty record.
  `--stream` writes `lines` and `nul` records as they arrive.
- `--type COLUMN=TYPE` converts a csv or tsv column to `int`, `float` or
  `bool` with the rules a definition's field of that type follows; an
  empty value is null and the other columns stay text. A value that is
  not the type is exit 3 with the line and the column, and a column the
  input does not have is exit 2.
- `jz new --string KEY=TEXT` writes TEXT as a string exactly as it is, so
  a value from a variable is never read as a file name (`@...`) or JSON,
  whatever it holds: `jz new --string "message=$MESSAGE"`.
- `jz new --text-file KEY=PATH` puts a file's text, or standard input's
  with `-`, into the JSON with every line ending kept. `KEY=@PATH` still
  drops the last one. Text that is not UTF-8 is exit 3.
- `jz new --path POINTER=VALUE` places a value at a JSON Pointer with
  the operators a plain argument has (`=`, `:=`, `=@`, `:=@`), making the
  objects and arrays on the way; `-` appends and an index names an
  element already given. `--string` and `--text-file` take a pointer in
  place of a key. A location given twice, a pointer into a value given
  whole, a key in an array and an index past the end are exit 2 before
  anything is read. `spec.replicas:=3` is still the key `spec.replicas`.

### Changed

- `jz --help` groups the options under Input, Output and the parser of
  command output, says what jz is for in one line, and shows examples of
  every source of JSON; `jz run --help` and `jz new --help` group theirs
  the same way. `--raw` is described as what it is, the text a definition
  cut without its field rules, and `--extract` and `--exclude` as keeping
  and dropping keys of each object.
- The README, the landing page and the usage guide describe jz as the
  step that makes JSON for the next command, and every jz command they
  show with its output is run by the end-to-end suite
  (`e2e/atago/docs.atago.yaml`); a test fails when a page shows one that
  no scenario runs. The `--explain=json` example said the df capture had
  8 lines read, where 7 are read and one is left out.
- A data file read with `--stream` (`jsonl`, `ltsv`, and the new `lines`
  and `nul`) leaves out a record it cannot read and goes on, as a stream
  of a command's output does, where it ended at the first. The status is
  still 3. Pass `--stop-on-error` to end at the first as before.

### Removed

- Breaking: `--yaml`, which wrote the output as YAML, is gone from the
  default mode, `jz run` and `jz new`. jz writes JSON only, and a command
  line that still passes `--yaml` is refused with exit status 2 and a
  message saying so, before anything is read or run. To keep getting
  YAML, pipe the JSON to a YAML tool: `df -h | jz | yq -P`. With
  `--stream`, read the JSON Lines one document at a time. Reading YAML is
  unchanged: a `.yaml` or `.yml` file, `--format yaml`, `jz new
  key:=@file.yaml` and definitions written in YAML all work as before.

### Fixed

- A csv or tsv read as data (`--format csv`, a `.csv` or `.tsv` file, `jz
  new key:=@file.csv`) keeps what it holds: an escape sequence stays in
  its value, where it was taken off as colour, and a line of spaces is a
  row, where it was dropped (`printf 'key\n   \nx\n' | jz --format csv`
  gave one row). A csv definition in the user registry no longer changes
  such a reading, which made `007` a number with `--format csv` and left
  it `"007"` with `jz new`; only `--parser csv` reads with it. `--explain`
  now says the file was read as csv rather than naming `csv/comma`.

## [0.3.0]

jz makes JSON from more than command output: from data files (CSV, TSV,
LTSV, JSON Lines, JSON and YAML, also gzip or bzip2 compressed) and from
arguments given to `jz new`. The registry is unchanged at 224 commands
and 596 definitions.

### Added

- `jz new` makes a JSON object from its arguments, or an array with
  `--array`: `key=text` is a string, `key:=json` a JSON value,
  `key=@path` a file's text, `key:=@path` the value a data file holds and
  `key[]=` one more element of an array. Nothing is guessed from a value,
  and a key given twice without `[]` is exit 2.
- Data files: `jz --file` reads a file as the format its extension
  names, `.csv`, `.tsv`, `.ltsv`, `.jsonl` (or `.ndjson`), `.json` and
  `.yaml` (or `.yml`), each also compressed as `.gz` or `.bz2`, and
  `--format NAME` reads piped data as that format. JSON keeps its key
  order and number literals, YAML is typed by the 1.2 core schema, and a
  key given twice, text that is not UTF-8 or a value JSON cannot hold is
  exit 3 with the line. `--stream` writes JSON Lines and LTSV records as
  they are read. `--explain` says which format was read and what named
  it, and `--explain=json` counts the values in `read.values`.
- The site has a [Cookbook](https://nao1215.github.io/jsonize/cookbook/)
  of 20 tasks, each run by the end-to-end suite, and a test fails when a
  documented command, the options block, the exit-code table or a demo
  tape no longer matches the command line.

### Changed

- A file given with `--file` whose extension names a data format is read
  as that format, where its text was detected as command output. Name
  `--parser` or `--define` to read such a file as a command's output;
  a file with any other extension (`captured.txt`) is detected as before.

## [0.2.0]

145 definitions for 55 more commands, 224 commands and 596 definitions
in all, the system commands of Windows and macOS among them. Where one
command's output is followed by another's, more definitions now refuse
the text instead of reading the other command's lines as their own.

### Added

- A tree definition can state what its top-level line looks like with
  `parse.root`. The node alternatives read every depth, and the one
  that reads the continuation of a wrapped list reads any text, so a
  word printed after `lspci -v`, or another command's output piped in
  behind it, was read as one more device with no children; with `root`
  it is refused, naming the line. `lspci -v`, `lsusb -v`, `lsusb -t`,
  `iw dev`, `apt-cache depends`, `systemctl list-dependencies` and
  `systemd-analyze critical-chain` state theirs.
- Definitions for 25 more traditional and modern commands. Archive
  listings: `gzip -l` and `-lv`, `xz -l` and `--robot
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
- A top-level `input.select.until` that states its whole line reads that
  line as the end of the output, the way `after` reads a heading, and a
  line after it is unread (exit 3). `net share` closes on its completion
  line with it, so another command's output behind the listing is
  refused rather than read as shares.
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
- Definitions for macOS: `vm_stat`, `sw_vers`, `diskutil list`,
  `launchctl list`, `pmset -g`, `netstat -i` and `netstat -rn`,
  `system_profiler SPSoftwareDataType` and `SPHardwareDataType`,
  `scutil --dns`, `networksetup -listallhardwareports`,
  `brew outdated --verbose`, `memory_pressure`, `top -l`, `otool -L`,
  `lipo -info`, `csrutil status`, `spctl --status`, `fdesetup status`
  and `shasum -a 256`, from output captured on a macOS 26 runner.
- Definitions for Windows: `tasklist` in its table, `/v`, `/svc`, `/m`,
  `/fo list` and `/fo csv` forms, `netstat -an`, `-ano` and `-e`,
  `route print`, `arp -a`, `getmac /v`, `driverquery` and `driverquery
  /v`, `sc query` and `sc queryex`, `schtasks /query` as a table and as
  a list, `net user`, `net localgroup`, `net share` and `net start`,
  `whoami /groups` and `/priv`, `chcp` and `ver`, from output captured on
  Windows Server 2022 and 2025 runners.
- Definitions for compiler-style diagnostics: `go vet` (and `go build`),
  `staticcheck`, `golangci-lint`, `actionlint` and `shellcheck -f gcc`;
  and for `mc ls` (the MinIO client) and `task --list`.
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

- `chage -l`, `curl --version`, `git count-objects -v` and `ethtool -i`
  are read only as the lines they print, `readelf -h` only as the fields
  of an ELF header, and `lscpu --caches` holds each row to a cache name
  and type. Lines another command printed after them were read as more
  fields and as more caches. The `readelf -h` schema names the fields it
  can hold, and is version 2.
- `systemctl list-sockets`, `list-paths`, `list-timers`,
  `list-automounts`, `list-machines`, `list-unit-files`, `list-jobs` and
  `list-units`, `networkctl list`, `loginctl list-sessions`,
  `list-seats` and `list-users`, `systemd-inhibit --list` and `tree`
  read their count line ("29 sockets listed.", "3 directories, 5
  files") as the end of the listing. Another
  command's output after it is refused as unread (exit 3), where rows of
  it that fit the columns were read as more entries.
- `jz run sysctl` and `jz run lsof` on macOS found no definition: both are
  the programs FreeBSD and Linux have, printing the same lines, and their
  definitions now apply there. `ls -le` and `ls -lO` on macOS, which add
  access control lists and file flags to a long listing, are refused by
  the argument rather than failing on the first list.
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
  this format, refuses `-c` and `--config-env` among its arguments, and
  on a pipe refuses a line whose parenthesis opens with `HEAD`, a tag or
  a remote branch. An `args` entry that ends in `=` matches the long
  option with any value.
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

- `docker system df` names the four kinds of usage it reports, and
  `docker version` keeps every value as the string docker printed,
  `Experimental`'s `false` included. Their output schemas are version 2.
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
- A `required` field whose value is a word `null_if` names is `null`, where
  it was an error: the command printed its word for no value, and
  `required` refuses a value that is missing or empty. `kubectl get
  services` reads a headless service's `None` cluster IP as null, which
  was refused; its output schema is version 2.
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
- `mise/outdated` reads a tool the configuration names and nothing
  installs, which mise prints as `[MISSING]`, with `current` as null. The
  row was refused before. Its output schema is version 2.

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
