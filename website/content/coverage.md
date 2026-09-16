---
title: Coverage
description: "Which forms of the commands people ask about most are read, which are read only when the parser is named, and which are not read and why."
toc: true
---

jz reads output formats, not commands. A command that changes its columns
with an option prints two formats and needs two definitions, so "jz
supports df" says less than it sounds like. This page says, for the
commands asked about most, what each form of their output does when it
reaches jz.

Three things can happen, and each of them is an answer:

| | what it means |
|---|---|
| read | piping the output in identifies it and converts it |
| named | use `jz --parser NAME` or `jz run`; some formats also need `--variant VARIANT` |
| refused | jz does not read it, and the reason is below |

"named" is not a shortcoming. A format whose text could be half a dozen
other things is one jz declines to guess at, because the failure worth
designing against is confident JSON from the wrong parser rather than
exit 4.

Everything below but the Windows section, which names its own machines,
was run on Ubuntu 26.04 (Linux 7.0) with `LC_ALL=C`:
GNU coreutils 9.7 and uutils coreutils 0.8.0, procps-ng 4.0.4,
util-linux 2.41.3, iproute2 6.19.0, systemd 259, sysstat 12.7,
BIND dig 9.20.24, iputils, BusyBox 1.37.0. net-tools was not installed,
so the `netstat` rows below were checked with BusyBox netstat, whose
socket and routing tables the existing definitions read, and the
interface table was written from the format strings net-tools prints it
with rather than captured. The `ifconfig`, `arp -a` and `rpm` rows were
checked against captures from containers (net-tools 2.10 on Debian;
rpm on Fedora 44, Rocky Linux 8 and 9 and openSUSE Leap 15.6) and from
the macOS 15 and 26 runners and a FreeBSD 15.1 virtual machine on
GitHub Actions.

## Storage and memory

| invocation | | definition |
|---|---|---|
| `df` | read | `df/gnu` |
| `df -k` | read | `df/gnu` |
| `df -a`, `df --total` | read | `df/gnu` |
| `df -h`, `df -H` | read | `df/gnu-human` |
| `df -m`, `df -B512`, `df --block-size=N` | read | `df/gnu-blocks` |
| `df -T` | read | `df/gnu-type` |
| `df -T -m` | read | `df/gnu-blocks-type` |
| `df -i` | read | `df/gnu-inodes` |
| `df -ih` | read | `df/gnu-inodes-human` |
| `df -P`, `df -PT` | read | `df/portable`, `df/portable-type` |
| `df --output=...` | refused | the columns are whatever the option named; `jz --parser table --variant aligned` reads it as a table |
| `free` | read | `free/gnu` |
| `free -h` | read | `free/gnu-human` |
| `free -w` | read | `free/gnu-wide` |
| `free -h -w` | read | `free/gnu-wide-human` |
| `free -t`, `free -l`, `free -s N -c N` | read | the variant the other options chose |
| `lsblk` | read | `lsblk/linux` |
| `lsblk -b` | read | `lsblk/bytes` |
| `lsblk -f` | read | `lsblk/filesystems` |
| `lsblk -t` | read | `lsblk/topology` |
| `lsblk -m` | read | `lsblk/permissions`; in a container without the device nodes, owner, group and mode are `null` together |
| `lsblk -P` | read | `lsblk/pairs` |
| `lsblk -r` | read | `lsblk/raw` |
| `lsblk -n` | named | `lsblk/no-headings`; with no header the text is a tree drawing and some words |
| `lsblk -J` | refused | it is JSON already |
| `lsblk -o ...` | refused | the columns are whatever the option named; `--parser table` reads it |
| `mount`, `mount -l`, `mount -t TYPE` | read | `mount/linux` |
| `/proc/mounts`, `/etc/mtab` | named | `jz --parser etc` reads them as `etc/fstab`, which is their format |
| `stat FILE` | read | `stat/gnu` |
| `stat FILE FILE` | read | `stat/gnu`, one object per file |
| `stat -f` | read | `stat/gnu-filesystem` |
| `stat -t` | named | `stat/gnu-terse`; a row of numbers describes too many things |
| `stat -c`, `stat --printf` | refused | the format is the one the option gave |

A figure df rounds for a person (`1.8T`) stays a string. The output does
not record the base the rounding used and the value is already rounded,
so converting it would invent precision the command did not print; run
`df` or `df -B1` for exact numbers. Where df counts in blocks, the unit
is in the heading and comes out beside the rows.

## Processes

| invocation | | definition |
|---|---|---|
| `ps` | read | `ps/posix` |
| `ps -ef` | read | `ps/unix` |
| `ps aux` | read | `ps/bsd` |
| `ps -el` | read | `ps/long` |
| `ps -eF` | read | `ps/full-format` |
| `ps -ely` | read | `ps/long-y` |
| `ps -eLf` | read | `ps/threads` |
| `ps axjf`, `ps -e --forest` | read | `ps/jobs`, `ps/posix` |
| `ps -eo ...`, `ps -o ...=` | refused | the columns are whatever the option named; `--parser table` reads it |
| `top -b -n1`, `top -b -n 5 -d 1` | read | `top/linux`, a record per iteration |
| `pidstat`, `pidstat -d`, `-r`, `-w` | read | the `pidstat` variants |

## Listings

| invocation | | definition |
|---|---|---|
| `ls -l` | read | `ls/long` |
| `ls -la`, `-lh`, `-lt`, `-lS`, `-ln` | read | `ls/long` |
| `ls -l --full-time` | read | `ls/full-time` |
| `ls -l --time-style=long-iso` | read | `ls/long-iso` |
| `ls -lZ` | read | `ls/long-context` |
| `ls -lR` | read | `ls/long-recursive` |
| `ls -log`, `ls -lgG` | read | `ls/long-no-owner-group` |
| `ls -lG`, `ls -lo` | named | `ls/long-no-group` |
| `ls -lg` | named | `ls/long-no-owner` |
| `ls -li` | named | `ls/long-inode`; `ls -ls` prints a number in the same place |
| `ls`, `ls -1` | named | `ls/names`; a list of lines is not evidence of anything |
| `ls -F`, `ls -p` | refused | the mark after a name is not part of the name and no rule can say which trailing character is one |

`ls -lG` and `ls -lg` print the same text: one drops the group column and
the other the owner, and nothing in the output says which name is left.
Both are reached by naming the variant, and `jz run ls -lG` picks the
right one from the arguments. Naming only the command reports both rather
than choosing.

## Network

| invocation | | definition |
|---|---|---|
| `ip address`, `ip -4 address`, `ip -6 address` | read | `ip/address` |
| `ip -br address`, `ip -o address` | read | `ip/brief-address`, `ip/oneline-address` |
| `ip link`, `ip -d link` | read | `ip/link` |
| `ip -br link` | read | `ip/brief-link` |
| `ip -s link`, `ip -s -s link` | read | `ip/stats-link`, `ip/stats-link-detail` |
| `ip route`, `ip -6 route` | read | `ip/route` |
| `ip neigh`, `ip rule`, `ip maddr` | read | the matching `ip` variants |
| `ip -j ...` | refused | it is JSON already |
| `ip tunnel`, `ip netns`, `ip xfrm` | refused | no definition; the subcommands are each their own format |
| `ss`, `ss -a`, `ss -l`, `ss -x`, `ss -tuln`, `ss -p` | read | `ss/linux` |
| `ss -e`, `ss -o`, `ss -m`, `ss -i` | read | `ss/linux`, with what the option added under `info` |
| `ss -t`, `ss -ua`, `ss -ul` | read | `ss/single-protocol` |
| `ss -u`, `ss -w` | read | `ss/connected` |
| `ss -s` | read | `ss/summary` |
| `ss -H` | refused | `-H` removes the header, which is the line that says what the format is |
| `netstat`, `-a`, `-t`, `-u`, `-x`, `-w`, `-l`, `-n`, `-e` | read | `netstat/all-sockets`, `netstat/internet`, `netstat/unix` |
| `netstat -r`, `netstat -rn` | read | `netstat/routing` |
| `netstat -e -r` | read | `route/linux`, which is the table those columns are |
| `route`, `route -n` | read | `route/linux`; a rejecting route's missing counts are `null` and its gateway and interface stay `-` |
| `route -e` | read | `netstat/routing`, the table `netstat -r` prints |
| `route -ee` | read | `route/linux-extended` |
| `route -A inet6` | read | `route/linux-inet6` |
| `route -C` | refused | the IPv4 routing cache is gone from current kernels, so the table has a header and no rows to write a definition against |
| `netstat -i` | read | `netstat/interface` |
| `netstat -s` | refused | which counters appear, in which order and at which indent comes from the kernel and the net-tools build; there is no capture here to write it against |
| `netstat` on Windows | refused | no definition; no vendor-published text sample was found to write one against, only screenshots |
| `dig NAME` | read | `dig/bind` |
| `dig NAME A NAME MX` | read | `dig/bind`, one entry in `replies` per query |
| `dig +nocmd` | read | `dig/bind`, with an empty `query` |
| `dig +nsid`, `+dnssec`, `+tcp`, `+norec` | read | `dig/bind` |
| a query that reached no server | read | `dig/bind`, with an empty `replies` and the attempt under `transport` |
| `dig axfr` | read | `dig/axfr` |
| `dig +noall +answer` | named | `dig/answers`; the same five columns are what a zone file holds |
| `dig +short` | refused | the output is the data of the records with nothing naming them |
| `dig +trace` | refused | the delegation steps have no line that opens one, so nothing says where a step ends |
| `ping -c N`, `-q`, `-D`, `-A`, `-s`, `-W`, `ping6` | read | `ping/linux`, `ping/bsd` |
| `ping -f` | refused | flood output is a line of dots, not records |
| `ifconfig`, `ifconfig -a`, `ifconfig IFACE` (net-tools) | read | `ifconfig/net-tools` |
| `ifconfig` (BusyBox) | read | `ifconfig/busybox` |
| `ifconfig`, `-a`, `-v`, `-L` (macOS, FreeBSD) | read | `ifconfig/bsd`; lines other than the flags, addresses and bit masks are kept in order under `properties` |
| `ifconfig -s`, `ifconfig -s -a` (net-tools) | read | `ifconfig/net-tools-short`, the table `netstat -i` prints without its banner |
| `ifconfig -l` | refused | a line of names |
| `arp -a`, `arp -an` | read | `arp/alternate` (Linux), `arp/freebsd`, `arp/darwin`, `arp/windows` |
| `arp`, `arp -n`, `arp -e`, `arp -i IFACE` (net-tools) | read | `arp/net-tools` |
| `arp -v` (net-tools) | refused | the closing count line is not read |
| `tracepath` | read | `tracepath/linux` |
| `traceroute` | refused | no definition |

## Packages and history

| invocation | | definition |
|---|---|---|
| `rpm -qi PACKAGE`, `rpm -qi A B`, `rpm -qia` | read | `rpm/info`, one object per package; a label rpm did not print is `null` |
| `rpm -qpi FILE.rpm` | read | `rpm/info`; the install date is the text `(not installed)` |
| `rpm -qi` naming a package that is not installed | refused | the `package NAME is not installed` line is not part of any block |
| `git log`, `git log --format=fuller` | read | `git/log` |
| `git log --stat`, `git log --shortstat`, `git show --stat` | read | `git/log-stat`, with `files` and `summary` under each commit |
| `git log --oneline` | read | `git/log-oneline` |
| `git log --numstat`, `--name-only`, `--summary`, `-p` | refused | a different listing under each commit |
| `git log --format=...` with a format of your own | refused | the text is whatever the format said, and git does not escape it |

## systemd

| invocation | | definition |
|---|---|---|
| `systemctl`, `systemctl list-units`, `--all`, `--plain`, `--failed` | read | `systemctl/units` |
| `systemctl list-unit-files` | read | `systemctl/unit-files` |
| `systemctl list-timers` | read | `systemctl/timers` |
| `systemctl list-sockets` | read | `systemctl/sockets` |
| `systemctl list-jobs`, including with nothing queued | read | `systemctl/jobs` |
| `systemctl list-machines` | read | `systemctl/machines` |
| `systemctl list-paths` | read | `systemctl/paths` |
| `systemctl list-automounts` | read | `systemctl/automounts` |
| `systemctl list-dependencies --plain` | read | `systemctl/dependencies` |
| `systemctl status UNIT` | read | `systemctl/status` |
| `systemctl show UNIT` | read | `systemctl/show` |
| `systemctl show` | named | `systemctl/show-properties`; `NAME=value` is also what `env` prints |
| `systemctl list-dependencies` | refused | without `--plain` a line carries a state marker and a drawing, and a line's depth then depends on both |
| `systemctl status` | refused | the whole-system report is a different format with no definition |
| `systemctl list-units --no-legend` | refused | the option removes the header, which is what says the format is this one |
| `systemctl cat UNIT` | refused | it is the unit file; `jz --parser ini` reads that |
| `journalctl -o short*` | read | the `journalctl` variants |

## Repeating reports

| invocation | | definition |
|---|---|---|
| `vmstat`, `vmstat N M`, `vmstat -w` | read | `vmstat/linux` |
| `vmstat -a`, `-d`, `-s` | read | `vmstat/linux-active`, `-disk`, `-stats` |
| `vmstat -f` | refused | one line saying how many forks there have been |
| `iostat`, `iostat N M` | read | `iostat/linux` |
| `iostat -x`, `-c`, `-d`, `-h` | read | `iostat/extended`, `cpu`, `device`, `human` |
| `iostat -t` and each of those with `-t` | read | the `-timestamped` variant of each |
| `mpstat`, `mpstat -I` | read | `mpstat/linux`, `mpstat/interrupts` |
| `sar -u`, `-r`, `-b`, `-n DEV`, … | read | the `sar` variants |

## Windows

jz builds and runs on Windows, and that is a different thing from reading
what Windows commands print. The rows below were checked against the
real commands on the GitHub-hosted Windows Server 2022 and 2025 runners,
English, with the console on code page 65001: the end-to-end suite runs
each command there on every change and compares what jz reports with
`hostname`, `ver` and PowerShell on the same machine, and a capture from
each release is kept as a fixture beside the output published by the
vendor.

| invocation | | definition |
|---|---|---|
| `ipconfig /all` | read | `ipconfig/all` |
| `ipconfig` | read | `ipconfig/windows` |
| `systeminfo`, `systeminfo /fo list` | read | `systeminfo/windows`; `/fo list` prints the same lines, and `jz run` refuses it on the argument, since `/fo` also names the csv and table forms |
| `systeminfo /fo csv` | named | `jz --parser csv --variant comma` |
| `ipconfig /displaydns` | refused | a different format; pinned as a refusal rather than read |
| `netstat` on Windows | refused | no vendor-published text sample was found to write one against, only screenshots |
| `net user`, `net localgroup`, `dir`, `route print`, `ver`, `tasklist`, `wmic` | refused | no definition |

Not checked, and not claimed: a Windows in another display language,
whose labels the signatures do not describe, so its output is refused
rather than read; a console code page other than UTF-8, where a name
outside ASCII is not valid UTF-8 and the input is refused; and a desktop
edition, which no runner provides.

A rounded figure with a separator and a unit (`16,384 MB`) stays a
string here for the reason it does everywhere else: the text does not say
which base it was rounded in. A label that holds a colon of its own
(`Virtual Memory: Max Size:`) keeps what follows its first colon as the
value, since a list of labels the definition has not seen cannot say
which colon is the separator.

## What is not read on purpose

- Output whose columns are chosen by an option (`ps -eo`, `df --output`,
  `lsblk -o`, `stat -c`). There is no format to describe: the caller
  already knows the columns, and `jz --parser table --variant aligned`
  reads an aligned table under whatever headings it has.
- Output with the header removed (`ss -H`, `systemctl --no-legend`,
  `lsblk -n` by automatic detection). The header is what says which
  format the text is; without it jz would be guessing. Leave the option
  out when piping into jz, or name the parser.
- Output that is already structured (`ip -j`, `lsblk -J`, `--format=json`
  anywhere). Converting JSON to JSON is not what this is for.
- A single word or number (`systemctl is-active`, `id -u`). There is
  nothing to convert.

## Compared with jc

[jc](https://github.com/kellyjonbrazil/jc) converts many of the same
commands, so it is a second opinion on what a text holds. The comparison
below was made against jc 1.25.7 (git 8290734, MIT licence). No jc
code or fixture is copied into this repository: the fixtures here were
captured or quoted independently, and jc is used as a tool to run against
them.

`scripts/compare_jc.py` runs both over the same saved input and reports,
per fixture, whether each produced JSON, how many records, and which
words of the input reached a leaf value of each result. It needs jc
installed (`python3 -m pip install jc`) and nothing else in this
repository depends on it. It compares against the input rather than
between the two documents on purpose: the two name and nest their keys
differently, and normalising that away would also hide a value one of them
dropped.

`--refused` turns the harness around and runs it over the fixtures a
definition is written to refuse: text of a neighbouring format, a row cut
short, a line the definition never looked at. The counts here are those
of the run that was made, and the registry has grown since. There were
47 such fixtures with a jc parser to compare against. jz refused 46 of
them, jc read 34. The one jz read is `wc -lwcL` output given to `jz
--parser wc --variant posix`, where the fourth count and a file name
beginning with a number are the same text and the arguments are the only
thing that separates them; `jz run wc -lwcL` refuses it on the argument.

Over the 380 fixtures a definition was written to read, jz read all of
them and jc refused 30. The refusals are a file system name containing a
space (`df`), every `ip address` fixture, an `ls -l` of a directory with
nothing in it, the headerless and raw forms of `lsblk`, most `ss` forms,
`pidstat`, `swapon`, an empty zip listing, `who` with an ISO time, and
the `systeminfo` excerpt.

Where both read a text, the differences worth knowing are these.

- A rounded number. `df -h` prints `13G`; jc reports `13958643712` and jz
  reports `"13G"`. The command rounded the value and did not record what
  it rounded, so the exact figure is not in the text; two rows whose
  sizes both round to `13G` come back from jc as the same number.
- A value made of parts. dig's `;; SERVER: 127.0.0.53#53(127.0.0.53)
  (UDP)` is one string in jc and three keys in jz (`server`,
  `server_name`, `protocol`). Both carry the information.
- A converted value. jz turns `Use% 7%` into `7`, `TIME 00:02:57` into
  `177` seconds and a timestamp with an offset into RFC 3339, so the
  original spelling is not in the output. jc keeps more of them as
  printed.
- A legend read as data. `systemctl list-units` closes with a legend
  explaining LOAD, ACTIVE and SUB; jc returns those lines as units, jz
  names them in `input.ignore` and returns the one unit the listing had.
- Several replies. `dig a.com A b.com MX` is two replies; jz reads both
  under one banner, jc reads both, and a text made of two whole dig runs
  pasted together is refused by jz because a run has one banner.

Formats jc reads that jz does not, and which stay out of scope for now:
shell and string values (`jwt`, `url`, `semver`, `path`, `timestamp`,
`email_address`), structured file formats that already have readers
everywhere (`xml`, `toml`, `plist`, `x509_*`; jz reads `yaml`, `json`,
`csv` and `jsonl` files as data, by extension), and a set of
commands with no definition here yet (`traceroute`, `iptables`,
`iwconfig`, `dmidecode`, `mdadm`, `ntpq`, `find`, `finger`,
`tune2fs`, `ufw`, `zpool status`, `zpool iostat`, `net user`, `net localgroup`, `dir`). The
string and file formats are a different job from reading what a command
printed: a JWT or a URL is not a command's output, and an XML or TOML
reader is the tool for a file that already has a grammar. Adding them would make the count of formats larger without
making the thing jz is for any better, which is why they are listed here
rather than written.

### Use cases from jc's articles

jc's author has published articles showing jc used on real work. The
ones whose input is a command's output are read by jz as well:

| article's pipeline | with jz |
|---|---|
| `rpm -qia`, then the licence, build date and description with jq | `rpm -qia \| jz` (`rpm/info`) |
| `git log --format=fuller --stat`, then files changed, insertions and deletions with jq | `git log --format=fuller --stat \| jz` (`git/log-stat`) |
| `ifconfig` in a script meant to run on Linux and macOS, then the interface name, IPv4 address and netmask | `ifconfig \| jz` (`ifconfig/net-tools`, `ifconfig/bsd`, `ifconfig/busybox`) |
| an ASCII table drawn with `+`, `-` and `\|` | `jz --parser table --variant box` |
| `ping -c1`, `arp -a` and `wc` while scanning a subnet | `ping/linux`, `ping/bsd`, the `arp` variants, `jz --parser wc` |

The keys are jz's own and are not meant to match jc's. The netmask is
kept as ifconfig printed it: `255.255.255.0` from net-tools and
`0xffffff00` from macOS and FreeBSD.

`scripts/compare_jc.py rpm git ifconfig arp table ping wc` runs both over
the fixtures of these commands. Both read all 63 that jz reads. Of the 17
written to be refused, jz refused 16 (the one it read is the `wc -lwcL`
case above) and jc read 16. The differences in what was read:

- rpm 6 prints the signature on the line under `Signature   :`. jc
  reports the signature as null there; jz keeps it.
- openSUSE's rpm prints `Distribution:` after the description, and jc
  reads that line as part of the description. So does the `package NAME
  is not installed` line `rpm -qi` prints for an unknown name, and a
  description line that repeats the `Name        : ` label becomes a
  second package. jz reads the first as its own key and refuses the other
  two.
- On macOS and FreeBSD jc writes a netmask in dotted form, which is not
  what ifconfig printed, and leaves out the far end of a point-to-point
  address (`inet 10.0.0.1 --> 10.0.0.2`).
- Under `git log --stat --summary`, jc lists the `mode change` line as a
  changed file. jz refuses the output.
- In an ASCII table whose value holds a bar (`a|b`), jc returns `a b`.
  jz refuses the row.

Other things the articles do are left out on purpose, because they are
not reading what a command printed:

- Working out a network from an address (`192.168.1.10/25` to its
  network, broadcast, host range, integer and hex forms, and whether it is
  private). That is a calculator.
- Decoding a certificate, a CSR or a CRL, and taking apart a URL, a path,
  a timestamp, an email address, a version string or a JWT.
- Adding values `date` did not print, such as the epoch, the day of the
  year or a 12-hour clock.
- Reading `typeset -p` and `declare -p`. Their quoting is the shell's
  own grammar, and the only complete reader of it is the shell.
- Plugins for Ansible, Salt or Nornir. jz writes JSON to standard output,
  which any of them can read.
- Selecting, sorting, grouping or reshaping the JSON. That is jq's job.

jz reads around ninety commands jc has no parser for, among them the
`systemd-*` tools, most of util-linux (`lsfd`, `lsns`, `lsmem`,
`lslocks`, `lsipc`, `lsirq`, `lsclocks`, `uuidparse`, `namei`,
`prlimit`), the `lsb_release`, `loginctl`, `localectl`, `hostnamectl`
and `resolvectl` family, package and language tooling (`apt-cache`,
`npm`, `cargo`, `go`, `rustup`, `uv`, `mise`, `snap`, `docker`),
`journalctl`, `sar`, `sensors`, `smartctl`, `pactl`, `nstat`, `tc`,
`bridge` and the `table`/`csv`/`kv`/`ini` shape readers.
