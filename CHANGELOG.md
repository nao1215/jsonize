# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

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
  complete document": a record that cannot be read still exits 3, but the
  records already written stay written.
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
- `input: fold:` joins a value that continues on the next line onto the
  line it belongs to, for the reports that break a long value at the
  terminal width (`ethtool`) and for the control files whose values
  continue on an indented line (`dpkg -s`, `apt show`).
- Official definitions for 126 commands in 263 output formats, covering
  coreutils and procps, the util-linux listings, the systemd tools, the
  classic network commands and the iproute2 ones, sysstat, the disk
  layout tools, the hardware and display tools, the archive and checksum
  tools, the account and firmware listings, the sound and font
  listings, git, docker, the package managers and the language
  toolchains. `jz list` prints the current set.

### Fixed

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

### Removed

- `min_jsonize`, `metadata.fixtures` and the kv `key_name`/`value_name`
  pair. No definition used any of them.
- `parse.on_mismatch`. `skip` dropped every line a pattern did not fit
  without saying which, which is the failure jz exists to prevent one
  line at a time: `host` was silently discarding the HTTPS record in its
  own fixture. A composite part now names the lines of its region that
  belong to a sibling with `ignore`, and a line nothing claims is an
  error.

### Decisions worth knowing

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
