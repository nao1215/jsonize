# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- Loading and validating a registry is measurably slower in the
  benchmarks (13 to 32 percent on `LoadRegistry`, 7 to 12 percent on
  definition validation), which is the definition struct having grown and
  the validator having more branches to walk. Parsing is unchanged.
  End to end the cost is 1.4 percent: converting `df` output through the
  built-in registry went from 78.3 ms to 79.4 ms, and ten of the
  definitions in that registry are new.

- `iostat`, `mpstat` and `pidstat` are read as an array of samples, one
  object per interval, instead of one object for the whole report. Asked
  for an interval those commands print their column header once per
  sample, and the second one reached the table as a row and failed to
  convert, so `iostat 1 3` was exit 3 — the input `iostat` is most often
  asked for could not be read at all. A single-shot run is the same shape
  with one sample in it, which is a breaking change to what those
  definitions return. `--stream` now writes one sample per line.
  The sysstat banner is dropped with it: it names the kernel, the host
  and the CPU count, which `uname -a` and `nproc` also print, and a shape
  with one object for the banner and an array for the rest has no
  streaming form. `vmstat` and `sar` print their header once and every
  sample under it, so they were already one table and are left alone.

### Added

- Thirty definitions for formats that had none: `/etc/services` and
  `/etc/protocols` (also read through `getent`), `nslookup HOST`,
  `aplay -l` (and `arecord -l`), `smartctl --scan`, `systemctl
  list-dependencies --plain`, `systemctl list-jobs`, `blkid -o export`,
  `git worktree list`, `lsusb -t`, `nmcli general status`, `mpstat -I`,
  `gpg --version`, `apt-cache depends`, `apt-cache policy`, `sensors -u`,
  `dpkg --get-selections`, `ip -s -s link`, `systemd-analyze
  critical-chain`, `systemd-analyze security`, `go tool dist list`,
  `journalctl -o short-monotonic`, `curl --version`, `amixer contents`,
  `openssl ciphers -V`, `locale -k`, `tree`, `gh auth status`,
  `mokutil --list-sbat` and `pactl list sinks`. Six of them are trees
  and one is a CSV, which is what the new parse types were for.
- `indent` accepts a list of the forms one level of a tree may take, for
  a report that marks a level with a branch character: `tree(1)` writes a
  level as one of "|   ", "    ", "|-- " and "`-- ", and
  `systemd-analyze critical-chain` as two spaces or a backtick and a
  dash. Each is a fixed width whose characters depend on whether anything
  follows, so a single string cannot count them.
- The duration units gain the week, which systemd prints.
- `parse.type: tree`, for a report whose depth comes from the input:
  `lspci -vv` prints a device, its capabilities under it and a
  capability's flags under those, and how far that goes is a property of
  the machine rather than of the format. The definition states what a
  node is — the fields read from its line, plus a `children` array — and
  the input states nothing but how deep the nodes go. Every line is a
  node, read by `node.parse` (an ordered list of regular expressions, or
  a key/value split), and `indent` states one level of indentation as it
  is written. A line that skips a level, indentation that is not a whole
  number of the unit, and depth beyond 32 are all errors. A tree may be a
  `composite` part and may not be a `records` part. `--stream` writes one
  top-level node per line.
- `lspci/verbose`, `lsusb/verbose` and `iw/dev`, the first definitions to
  use it. `lspci/kernel` and `lsusb/linux` gained one exclusion each,
  because the output they read is a subset of the verbose output and only
  the register dumps and descriptor blocks say which of the two a text
  is.

### Fixed

- A file's path could name a definition that describes a shape, and those
  carry no signature, so nothing about the text could rule one out: `df`
  output in a file called `comma` inside a directory called `csv` was
  read as CSV and returned at exit 0, where the same bytes on standard
  input were read as `df`. Wrong JSON at a successful exit is the one
  failure this tool exists to prevent. A path now names only a definition
  that makes a claim the text can refuse.
- A path that named a definition whose signature accepted the text but
  whose parse then failed gave exit 3, where the same bytes on standard
  input were read correctly. The fallback ran only when the choice
  failed, not when the reading did, so the guarantee that a path can only
  add an answer was not kept. Both failures now drop the path.

- Checking a registry took 25 seconds where it had taken one, as soon as
  a deeply nested fixture was added. go-cmp builds its report as it walks
  a value, and on a 22-level tree that runs for tens of seconds whether
  the values agree or not. The comparison is now made first and described
  only when it fails, and the description is by line rather than by
  structure: a golden mismatch is two pretty-printed documents, and where
  they part company is what a reader wants.
- `ip route` read the type a route may open with as the destination.
  The main table prints none, because every route in it is unicast;
  `ip route show table all` prints the local table too, whose routes are
  local, broadcast, anycast and multicast, and every one of them failed.
  The list of types is the one the kernel has rather than the ones seen
  so far: the first pass took it from one machine's IPv4 table and missed
  anycast, which is printed only for IPv6.
- `ip -s -s link` was claimed by the definition for one `-s` and then
  failed on the error tables the second one adds. The two are now
  separate definitions, and each refuses the other's output.
- `ip link`, `ip -s link` and `ip address` refused an interface that
  carries a free-text alias, which is a line `ip` prints between the link
  layer address and the alternative names. `ip link` read it as one more
  alternative name and failed; `ip -s link` counted lines to find its
  counter tables, and the extra line moved them. The tables are now found
  by their header, the alias is a part of its own, and each of the two
  parts under the link line names the other's lines as belonging to a
  sibling rather than taking a position, since which of them comes first
  is not something the format promises.

- The streaming reader trimmed the carriage return of a CRLF line ending
  before stripping escape sequences, where the whole-document reader
  strips first. A record that ended with an escape sequence around the
  carriage return was read differently by the two.

- `--define YAML`, which takes the definition itself instead of the name
  of one: a `parser.yaml` without `format`, `command`, `variant` or
  `detect`, so what is left is `parse` and optionally `input` and
  `fields`. Nothing is detected, so it cannot be combined with `--parser`
  or `--variant`. It adds no expressive power — the body goes through the
  same loader and validation as a file — so a definition worth keeping
  moves into a registry unchanged.
- Definitions that describe a shape rather than a command:
  `table/whitespace`, `table/aligned`, `table/box`, `csv/comma`,
  `csv/tab`, `kv/colon`, `kv/equals` and `ini/default`. They are
  `--define` under a name and are never reached by automatic detection.
  Such a definition makes no claim about the text, so the conformance
  runner leaves it out of the check that asks whether a definition reads
  a neighbour's output, and requires the variant to be named rather than
  the parser alone.
- `parse.type: csv`, with RFC 4180 quoting and a `delimiter` (default
  `,`), so a value may contain the delimiter, a quote written twice or a
  line break. A short row leaves the remaining keys null and a long one
  is an error; a header naming a column twice numbers the repeats.
- `parse.type: ini`, reading `[section]` headings and `key = value`
  lines into an object of objects. Keys before the first heading go under
  the empty name, `#` and `;` start a comment only at the start of a
  line, a repeated section continues the first and a repeated key takes
  the last value.
- `table.split: box`, for a table drawn with `+-=~` or any character of
  the Unicode Box Drawing block. The bars mark the cells, and the rules
  separate the header from the body: inside the body a line is a row of
  its own, which is what MySQL, psql, `sqlite3` in box mode and `duf`
  print, and a line whose first cell is empty continues the row above it,
  which is how a table that wraps a long value writes the rest. A header
  written over two lines is one name joined with `_`, and an empty cell
  is null.

### Fixed

- `StripANSI` would take a line break as the final byte of an escape
  sequence and eat it, joining two records into one. It did so only when
  the whole document was read, since the streaming reader has already cut
  the line off, so the two readings disagreed on text as small as
  `"\x1b\n"`. No escape sequence ends with a line break or a carriage
  return, so none may consume one. Found by the fuzz target added for the
  new parse types.
- The streaming reader checked that a record was valid UTF-8 before
  stripping its escape sequences, where the whole-document reader strips
  first. A record whose only invalid bytes were inside an escape about to
  be removed was refused by one reading and accepted by the other.

- `fields.<name>.type: duration`, which turns a printed length of time
  into seconds: `3-04:05:06`, `04:05:06`, `04:05`, `01:23.45`, `13:42m`,
  `13 days, 4:30`, `45 min`, `3days`, `1h2m3s`, `3d4h`. The result is an
  integer when the length is a whole number of seconds. `layout` is
  required and is `h:mm` or `mm:ss`, saying what the last part of a bare
  two-part reading is, since `ps` prints four minutes fifty seconds as
  `4:50` and `uptime` prints an hour and twenty-three minutes as `1:23`.
  Applied to the TIME column of every `ps` variant and to the uptime of
  `uptime`, `w` and `top`, whose expected JSON changed from a string to a
  number. `top`'s TIME+ stays text: it falls back from `mmm:ss.hh` to
  `hhh,mm` for a long-running task, so the column would carry two
  different precisions.
- `--assume-year YEAR` (or `now`) and `--assume-zone ABBR=+HHMM`
  (repeatable), which supply what a format leaves out. A time field whose
  layout carries no year now writes `year: assumed` beside it, which says
  the format prints none; the value stays the string it was printed as
  until `--assume-year` says which year to read it in. A layout naming
  its zone with `MST` converts when the abbreviation states its own
  offset (`UTC`, `GMT`) and otherwise waits for `--assume-zone`. Applied
  to `who`, `last`, `journalctl -o short`, `date` and `timedatectl`.
  Nothing reads a clock or a zone database on the way past: `now` is
  resolved once when the command line is read, and whatever Go made of an
  abbreviation is discarded, since Go resolves one against the running
  machine and the same text would convert differently on two machines.
- `date` reports the whole reading as `timestamp` beside the parts it is
  made of.

- `--raw`, which skips the field rules so that every value is the text it
  was extracted from. A value that comes out wrong was either cut from
  the wrong place or converted wrongly, and once it is typed the two look
  alike; this separates them. Nothing is converted, `trim_prefix` and
  `null_if` are not applied, and `required` and `when_missing` are left
  out with them, so the shape does not change with the values in it. It
  is not offered on `jz test`, which compares a fixture against the typed
  JSON beside it. The option existed before release and was removed as a
  second output shape for a consumer to choose between; it is back for the
  other reader, the one writing the definition.

- `--explain`, on every mode, which writes the chosen definition and what
  it was chosen on to standard error: the definition and the registry it
  came from, the conditions the input met, and the definitions that were
  left out with the reason for each. Standard output and the exit status
  are untouched, and no clock reading is written, so two runs over the
  same input explain themselves identically. A search of the whole
  registry lists only the definitions that came close, since rejecting
  three hundred others on the first expression of a signature explains
  nothing; a search scoped to one parser lists every variant.
- A file path is read as evidence about the text in it: the directory
  names the parser and the file names the variant, and the pair has to
  exist in the registry. `jz --file /etc/fstab` and
  `jz --file /proc/meminfo` now convert without the parser being named,
  which is what the formats too unremarkable to claim on sight needed.
  Nothing in the code knows about `/etc` or `/proc`; those work because
  parsers of those names exist. The path stays evidence: a definition it
  names that the text does not fit is dropped and the text is read on its
  own terms, so a path can only add an answer.

- `--stream` reports a record it cannot read and carries on with the next
  one, instead of ending there. The line is named on standard error as
  `jz: <definition>: line N: <reason>`, the records after it are still
  written, and the status is 3 at the end if anything was skipped. This is
  what the commands `--stream` exists for actually print: `ping` puts a
  request timeout among its replies and `rsync` puts progress among its
  file names, and ending at the first of those threw away everything still
  to come. Reading a whole document is unchanged — one record that does
  not fit still means the text is not the format it claimed to be, so
  nothing is written. `engine.Stream` takes an
  `onError func(*ParseError) error` beside `emit` so a library caller
  chooses between the two.

- A decoy corpus under `registry/testdata/decoys/`, checked by
  `make registry-test`. It holds text that belongs to no format jz reads
  and every definition is applied to every file of it. The machinery
  already existed for third parties as `jz test --decoys DIR`; what was
  missing was a corpus of its own for the official registry, so the texts
  that had defeated a signature once lived in a scratch directory and
  were checked by hand. The first thing it caught on being wired in was
  `etc/crontab` reading a shell script.
- Automatic detection as the default behaviour: `COMMAND | jz` and
  `jz < FILE` identify the format from the text and convert it.
- `jz run COMMAND` for the case where jz starts the command itself,
  passing standard input through, forwarding standard error and mirroring
  the exit status.
- `jz list`, `jz list COMMAND` and `jz list COMMAND VARIANT`, with
  `--json` and `--sources`.
- YAML parser definitions (format 1) with table, regex, key/value and
  composite parsers, ordered regex alternatives, NUL-separated records and
  typed field conversion.
- Layered registries: `JSONIZE_REGISTRY_PATH`, the user directory and the
  registry embedded in the binary. jz makes no network access.
- `--extract KEY` and `--exclude KEY`, repeatable, which keep or drop the
  named keys of every object jz prints. Naming a key the format does not
  produce is an error listing the keys it has, and the two options cannot
  be combined.
- `--stream`, on the default mode and on `jz run`, for a command that
  keeps printing: one JSON document per line, each written as soon as the
  record behind it is complete. It applies to the formats that yield
  records (`table`, `regex` per line, a kv list, `records`); a format read
  into one object is refused, as is `--pretty`. This is the only option
  that changes the output contract, from "standard output carries a
  complete document or nothing" to "every line of standard output is a
  complete document".
- `/proc/meminfo`, `/proc/loadavg`, `/proc/uptime`, `/proc/cpuinfo` and
  `/proc/diskstats`, as variants of a parser named `proc`, read from a
  pipe or a redirect the way the `/etc` files are. All but one say enough
  about themselves to be identified from their text; `/proc/uptime` is
  two decimal numbers and nothing else, so it is named rather than
  claimed on sight.
- `/proc/cpuinfo` is `cpuinfo-x86`, because the file has no shape common
  to the architectures: arm64 writes `Features` and `CPU implementer`
  where x86 writes `flags` and `vendor_id`. The variant name is what
  says which one jz reads, and an arm64 machine gets exit 4 rather than
  a block with most of its fields missing.
- `/proc/diskstats` is read for the twenty-field row Linux 5.5 and later
  write. The fourteen- and eighteen-field rows older kernels write are
  refused as rows with too few fields, so nothing shifts and no counter
  is filled in. Those shapes are not covered because no machine running
  such a kernel was available to capture from; the format could describe
  them, and whether to do that with one definition or a variant per shape
  is an open question in design.md.
- `parse: type: records` for a report whose blocks repeat: a line
  matching `start` opens a record and the `parts` are applied to each. A
  `composite` part may itself be `records`, which is what a report that
  opens with a header block and then repeats a block needs.
- `aliases:` on a definition, for the commands that print a format
  another command prints: `vdir` prints what `ls -l` prints, `printenv`
  what `env` prints, `getent passwd` what `/etc/passwd` holds, `podman`
  and `nerdctl` what `docker` prints, and the g-prefixed coreutils macOS
  installs print what the coreutils commands print. `jz run`,
  `--parser` and `jz list` all take the other name.
- `type: time` for a field, with `layout` (a Go reference layout,
  required, and it has to state a year) and `location` (`utc` by default,
  or `local`). The value is written as an RFC 3339 string and never as an
  epoch number. It is applied where the text says what it means:
  `journalctl -o short-iso`, `stat`, `mtr --report` and `date -R` all
  print a numeric offset. A timestamp with no year (`who`, `last`,
  `journalctl -o short`), one with only a zone abbreviation (`date`,
  `systemctl list-timers`, `journalctl --list-boots`, `timedatectl`) and
  one with no zone at all (`tar -tv`, `unzip -l`) stay the strings they
  were printed as, because dating them means inventing what the output
  does not say.
- `input: fold:` joins a value that continues on the next line onto the
  line it belongs to, for the reports that break a long value at the
  terminal width (`ethtool`) and for the control files whose values
  continue on an indented line (`dpkg -s`, `apt show`).
- Official definitions for 160 commands in 355 output formats, covering
  coreutils and procps, the util-linux listings, the systemd tools, the
  classic network commands and the iproute2 ones, sysstat, the disk
  layout tools, the hardware and display tools, the archive and checksum
  tools, the account and firmware listings, the sound and font listings,
  the binary inspection tools (`nm`, `objdump`, `size`, `ar`, and the
  three hex dumps `xxd`, `hexdump -C` and `od`), the process attributes
  (`taskset`, `chrt`, `ionice`, `capsh`, `getcap`, `namei`), git
  including its plumbing, openssl, docker, the package managers, the
  language toolchains, and two dozen files under `/proc` read the way the
  `/etc` files are, plus a handful that describe a shape rather than a
  command (`table`, `csv`, `kv`, `ini`). `jz list` prints the current set.

### Fixed

- The suggestion for a parser name that does not exist offered every
  command within two edits, alphabetically, capped at three. At a few
  hundred commands that buries the one the caller meant and can drop it
  entirely: `--parser dg` answered "did you mean ar, df, dig", where `ar`
  is two edits away and the other two are one. Only the closest are
  offered now, and a name that extends a command is ranked by the same
  distance as everything else, so `--parser lscpuu` says lscpu rather
  than lscpu and ls together.
- `git/log-oneline` read a symbol table. `nm -D` prints a zero padded
  sixty-four bit address, a one letter symbol type and a name, which is a
  hexadecimal word followed by a line of text; the definition claimed
  seven to twenty hexadecimal digits and so reported 0000000000000000 as
  a commit hash. The bound is fifteen digits now, which is past what any
  repository's abbreviation reaches and short of where an address column
  starts.
- `file/posix` read a dump of a file's bytes. An xxd line is a word, a
  colon and a line of text, and any dump has some line whose printable
  column spells one of the kinds file(1) names, so a hex dump was read as
  a listing of sixteen byte long paths. Every description must name a
  kind now, or be one of the two messages file(1) writes for a file it
  could not identify; one line naming a kind is still required as well,
  which is what keeps a listing of nothing but errors refused.
- `etc/crontab` accepted a shell script that installs a cron entry. One
  entry is not evidence: a script that installs one carries one, and so
  does a document that quotes one. The signature now claims the whole
  text, which is entries, variable assignments, comments and blank lines
  and nothing else, so the refusal happens before parsing rather than on
  the first line that is not an entry.
- `git log --stat`, `--numstat`, `--shortstat`, `--name-only`,
  `--name-status` and `-p` were read as `git/log` and their file lists,
  diffstats and diffs were reported as lines of the commit message. The
  message part took whatever followed the blank line; it now requires
  what git actually prints, which is a message indented by four spaces.
  None of those six formats is read: a diffstat elides its paths and pads
  its columns, so there is nothing to recover the file names from. This
  is not something a signature could have settled, because the leading
  lines of such a listing are an ordinary commit block.
- `etc/crontab` read the other lines of a shell script that installs a
  cron entry. A signature only has to find one entry to vouch for a file,
  and the five time fields of the parse pattern took any word, so
  `echo "installed ..."` became an entry with `echo` as its minute. The
  time fields now repeat the vocabulary the signature checked for.
- `ethtool` and `mtr` signatures that looked for their opening line
  alone, which prose explaining the format carries as a quotation. Each
  now also requires the thing the report is made of: an indented property
  under `Settings for eth0:`, and a hop line under the `HOST:` header.
- Signatures that accepted the output of a neighbouring command. `group`
  read `/etc/passwd`, `uname -a` and `timedatectl` output; `hosts` read
  the clock at the start of BSD `uptime` and `w` output as an IPv6
  address; `env` claimed `/etc/os-release` under automatic detection and
  kept the shell quotes inside the values; `file` read `blkid` output and
  any `token:` line followed by a newline; `ip -brief address` read
  `ip -brief link` output, putting the MAC address in the address list.
- The same failure in the definitions added later, each found by
  applying every definition to every other definition's fixtures.
  `cksum` read a crontab entry as a checksum and a size; `etc/passwd`
  read the first line of BusyBox `ifconfig` and a `gpg --with-colons`
  key; `etc/fstab` read a mount table header as an entry; `file` read
  `udevadm`, `ip rule`, `bridge link`, `nmcli` and Debian control
  records; `iostat` read the extended device table with the definition
  for the CPU one; `etc/crontab` read a user crontab, whose command
  lands where the user name belongs.
- The sysstat reports asked for with an interval. `mpstat`, `pidstat`
  and `iostat` had only ever been captured without one, so none of them
  had seen the summary block an interval adds, which prints the column
  names again and labels its rows Average.
- Definitions that listed arguments their command does not need. `swapon`
  and `systemctl` print with no arguments exactly what their definition
  reads, and both turned that call away; the `ls` list enumerated flag
  combinations that bundling already covers and left out the long
  spellings. `lsattr` refused the relative path it prints when given no
  operand, `ss` had no pattern for the unix sockets a bare call lists,
  and `systemctl` on current systemd closes with a legend the ignore list
  did not cover.

### Changed

- `env` is read only when the parser is named. `NAME=value` is the shape
  of a `.env` file, a properties file and a shell fragment too, and every
  one of those that came along wanted another exclusion inside `env`.
- `passwd`, `group`, `hosts` and `os-release` describe files rather than
  commands and are now variants of `etc`. Only `passwd` also existed as a
  binary, and `jz run passwd` used to start the interactive one.
  `/etc/os-release` no longer needs a name: `PRETTY_NAME` together with
  `NAME` and `ID` is that file and close to nothing else.
- `jz run` answers a command that succeeded without printing anything
  with `[]`, where the format it named has an empty form.
- A kv definition can convert the values of labels that are not
  identifiers (`CPU(s)`, `Thread(s) per core`).
- `expect_error` in a fixture covers a definition refusing text at
  detection, not only at parse time.
- `internal/registry`, `internal/definition`, `internal/selector`,
  `internal/engine`, `internal/convert` and `internal/jsonutil` moved to
  `pkg/`, so another program can load a registry, select a definition and
  parse with it. `internal/cli`, `internal/runner`,
  `internal/conformance` and `internal/buildinfo` stay internal: they are
  decisions about a command line, not about reading text.

### Removed

- `min_jsonize`, `metadata.fixtures`, `metadata.authors` and the kv
  `key_name`/`value_name` pair. No definition used any of them, and
  nothing read them back: `authors` was documented in the definition
  format and reported by neither `jz list` nor `jz list --json`.
- `parse.on_mismatch`. `skip` dropped every line a pattern did not fit
  without saying which, which is the failure jz exists to prevent one
  line at a time: `host` was silently discarding the HTTPS record in its
  own fixture. A composite part now names the lines of its region that
  belong to a sibling with `ignore`, and a line nothing claims is an
  error.

### Decisions worth knowing

- A signature says what a format is, not what it is not. `none` is a
  safety valve for the case where there is no positive form to write,
  with the reason beside it, rather than the way a definition keeps its
  neighbours out. A list of what a format is not cannot be finished, it
  grows by one with every definition somebody adds, and it couples the
  two together: `file/posix` had collected fourteen such expressions and
  now has none. Of the 68 the registry carried, 31 excluded nothing at
  all, 13 named another command, and 11 remain, every one telling
  variants of a single command apart.
- Adding a definition needs nothing but its own signature and its own
  fixtures. Needing to edit somebody else's definition means the two
  were coupled through an exclusion, and `jz test` is what shows it.
- A signature may narrow what a definition undertakes to read, and that
  is a decision to write down. `git log --oneline` is read for an
  abbreviated hash of seven to twenty digits, `cksum` for a checksum
  written in full, and `file` for the kinds of file named in its
  signature. What falls outside is refused with exit 4, which is an
  answer; the failure to design against is text read confidently with
  the wrong definition and returned with status 0.
- Fixtures verify a signature; they do not decide it. The order is
  decide the format, write the signature, capture fixtures that show it.

- A rounded, human-readable size (`df -h`, `free -h`, `ls -lh`, `lsblk`)
  is reported exactly as printed. The base is not recoverable from the
  output and the value is rounded, so a byte count would be invented.
- A format too generic to recognise (`du`, `wc`) is only used when the
  parser is named; its signature is still checked then.
- Naming a parser or variant never disables the signature check.
- The public options are `--file`, `--pretty`, `--parser`, `--variant`
  and `--help`. The input limit is 64 MiB and is not configurable.
- Two commands that print the same format are one definition with an
  alias, not two definitions. Nothing in the text says which of them
  wrote it, so a second definition would read the first one's output and
  the answer would be a guess about the command rather than a reading of
  the text. The alias carries the arguments its name needs, which is how
  `getent passwd` and `getent group` reach different files.
- Format 1 is not frozen until the first tag. After it, adding a key is a
  minor release and the number stays 1; an older jz refuses a definition
  that uses a new key with an unknown-key error saying a newer jz may be
  needed. Removing a key or changing what one means is format 2, because
  neither can be told from a mistake by looking at the file.
- A tree of arbitrary depth has no shape jz can promise in advance and
  is left out: `npm ls --all`, `iw dev`, `lsusb -t`, `docker info`,
  `apt-cache policy`, `systemd-analyze critical-chain`. Most of those
  commands print JSON of their own.
