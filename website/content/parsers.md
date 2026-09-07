---
title: Parsers
description: The commands jsonize reads today, and how it decides which definition applies.
toc: true
---

`jz list` prints the table below from the binary you have. `jz list df`
shows the variants of one command and `jz list df gnu` everything about
one definition, including where it came from and how it detects its
input.

A handful of entries describe a shape rather than a command: `table`
(`whitespace`, `aligned`, `box`), `csv` (`comma`, `tab`), `kv` (`colon`,
`equals`) and `ini` (`default`). They are never chosen on their own —
two words above two words says nothing about what produced them — and
are reached by naming both halves:

```console
$ docker ps | jz --parser table --variant box
$ jz --parser ini --file ~/.gitconfig
```

They convert nothing: a shape says nothing about what its columns mean,
so every value is the text it was cut from. Use `--define` where you want
the same reading with settings of your own.

## Supported commands

| Command | Variants |
|---------|----------|
| `amixer` | `simple-controls` |
| `apt` | `list`, `show` |
| `apt-cache` | `stats` |
| `ar` | `table-verbose` |
| `arp` | `alternate` |
| `blkid` | `linux` |
| `bluetoothctl` | `show` |
| `bridge` | `fdb`, `link` |
| `busctl` | `list` |
| `capsh` | `print` |
| `cargo` | `command-list`, `install-list` |
| `chage` | `linux` |
| `chrt` | `policy` |
| `cksum` | `posix` |
| `csv` | `comma`, `tab` |
| `date` | `posix`, `rfc-email` |
| `debconf-show` | `linux` |
| `df` | `bsd`, `bsd-human`, `busybox-human`, `gnu`, `gnu-human`, `gnu-inodes`, `gnu-inodes-human`, `gnu-type`, `gnu-type-human` |
| `dig` | `bind` |
| `docker` | `images-disk-usage`, `images-repo-tag`, `network-ls`, `ps`, `stats`, `volume-ls` |
| `dpkg` | `list`, `status` |
| `du` | `gnu-human`, `posix` |
| `efibootmgr` | `linux` |
| `env` | `null-separated`, `posix` |
| `etc` | `crontab`, `fstab`, `group`, `hosts`, `nsswitch`, `os-release`, `passwd`, `resolv-conf` |
| `ethtool` | `driver-info`, `features`, `settings`, `statistics` |
| `factor` | `default` |
| `fc-list` | `posix` |
| `fdisk` | `linux` |
| `file` | `posix` |
| `findmnt` | `df`, `linux`, `source-first` |
| `free` | `gnu`, `gnu-human`, `gnu-wide` |
| `getcap` | `paths` |
| `getconf` | `glibc` |
| `getfacl` | `posix` |
| `git` | `branch-verbose`, `config-list`, `count-objects`, `diff-numstat`, `diff-stat`, `for-each-ref`, `log`, `log-oneline`, `ls-files-stage`, `ls-tree`, `remote-verbose`, `shortlog-summary`, `show-ref`, `stash-list`, `status-porcelain` |
| `go` | `env`, `list-modules`, `mod-graph`, `version-modules` |
| `gpg` | `colons` |
| `hciconfig` | `linux` |
| `hexdump` | `canonical` |
| `host` | `bind` |
| `hostnamectl` | `linux` |
| `iconv` | `list` |
| `id` | `posix` |
| `ifconfig` | `busybox` |
| `ini` | `default` |
| `ionice` | `class` |
| `iostat` | `cpu`, `device`, `extended`, `linux` |
| `ip` | `address`, `brief-address`, `brief-link`, `link`, `multicast-address`, `neighbour`, `route`, `rule`, `stats-link` |
| `ipcs` | `linux`, `message-queues`, `semaphores`, `shared-memory` |
| `iw` | `link` |
| `journalctl` | `boots`, `short`, `short-iso`, `short-precise` |
| `kv` | `colon`, `equals` |
| `last` | `busybox` |
| `ldconfig` | `cache` |
| `ldd` | `posix` |
| `locale` | `posix` |
| `localectl` | `linux` |
| `loginctl` | `seats`, `sessions`, `users` |
| `losetup` | `linux` |
| `ls` | `long`, `long-context`, `long-inode`, `long-no-owner-group` |
| `lsattr` | `linux` |
| `lsb_release` | `linux` |
| `lsblk` | `bytes`, `filesystems`, `linux`, `pairs`, `topology` |
| `lsclocks` | `linux` |
| `lscpu` | `caches`, `extended`, `linux` |
| `lsfd` | `linux` |
| `lsipc` | `linux` |
| `lsirq` | `linux` |
| `lslocks` | `linux` |
| `lslogins` | `linux` |
| `lsmem` | `linux` |
| `lsmod` | `busybox`, `linux` |
| `lsns` | `linux` |
| `lsof` | `linux`, `tasks` |
| `lspci` | `kernel`, `linux`, `machine`, `numeric`, `numeric-names`, `verbose-machine` |
| `lsusb` | `linux` |
| `md5sum` | `posix` |
| `mise` | `ls` |
| `modinfo` | `linux` |
| `mount` | `bsd`, `linux` |
| `mpstat` | `linux` |
| `mtr` | `report` |
| `namei` | `long` |
| `netstat` | `all-sockets`, `internet`, `routing`, `unix` |
| `networkctl` | `list` |
| `nm` | `dynamic` |
| `nmcli` | `connection`, `device`, `device-show`, `wifi` |
| `npm` | `ls`, `outdated` |
| `nstat` | `counters` |
| `numactl` | `hardware` |
| `objdump` | `section-headers` |
| `od` | `hex-bytes` |
| `openssl` | `ciphers`, `version-all` |
| `pactl` | `info`, `short-devices` |
| `parted` | `machine` |
| `pidstat` | `linux`, `memory` |
| `ping` | `linux` |
| `pip` | `columns` |
| `pmap` | `linux` |
| `proc` | `buddyinfo`, `consoles`, `cpuinfo-x86`, `crypto`, `devices`, `diskstats`, `filesystems`, `interrupts`, `loadavg`, `meminfo`, `modules`, `mountinfo`, `net-arp`, `net-dev`, `net-route`, `net-unix`, `partitions`, `schedstat`, `self-limits`, `self-status`, `softirqs`, `stat`, `uptime`, `vmstat` |
| `ps` | `bsd`, `busybox`, `posix`, `unix` |
| `readelf` | `header` |
| `resolvectl` | `status` |
| `rfkill` | `linux`, `list` |
| `route` | `linux` |
| `rsync` | `itemize` |
| `rustup` | `component-list`, `target-list`, `toolchains`, `toolchains-verbose` |
| `sar` | `cpu`, `filesystem`, `io`, `memory`, `network-device`, `paging`, `queue`, `swap`, `task` |
| `scc` | `default` |
| `sensors` | `linux` |
| `service` | `status-all` |
| `sfdisk` | `dump` |
| `sha1sum` | `posix` |
| `sha224sum` | `posix` |
| `sha256sum` | `posix` |
| `sha384sum` | `posix` |
| `sha512sum` | `posix` |
| `size` | `gnu` |
| `snap` | `list` |
| `ss` | `linux`, `single-protocol`, `summary` |
| `ssh-keygen` | `fingerprint` |
| `stat` | `gnu`, `gnu-filesystem`, `gnu-terse` |
| `swapon` | `legacy`, `linux` |
| `sysctl` | `linux` |
| `systemctl` | `show`, `sockets`, `status`, `timers`, `unit-files`, `units` |
| `systemd-analyze` | `blame`, `time` |
| `systemd-cgtop` | `batch` |
| `systemd-id128` | `show` |
| `systemd-path` | `paths` |
| `table` | `aligned`, `box`, `whitespace` |
| `tar` | `busybox`, `gnu` |
| `taskset` | `affinity-list` |
| `tc` | `qdisc`, `qdisc-stats` |
| `timedatectl` | `linux`, `timesync`, `timezones` |
| `top` | `linux` |
| `tracepath` | `linux` |
| `udevadm` | `info` |
| `ulimit` | `bash`, `dash` |
| `uname` | `darwin`, `linux` |
| `unzip` | `busybox`, `info-zip` |
| `update-alternatives` | `query`, `selections` |
| `upower` | `device`, `dump`, `enumerate` |
| `uptime` | `bsd`, `linux` |
| `uuidparse` | `linux` |
| `uv` | `pip-show`, `tool-list` |
| `vmstat` | `linux`, `linux-active`, `linux-disk`, `linux-disk-summary`, `linux-stats` |
| `w` | `bsd`, `linux`, `linux-short` |
| `wc` | `posix` |
| `who` | `posix` |
| `xrandr` | `linux`, `listmonitors` |
| `xxd` | `default` |
| `zipinfo` | `default` |

A variant is one output format of a command. GNU `df`, `df -h`, macOS
`df` and BusyBox `df -h` are four formats, so they are four definitions.
When several implementations print the same format, one definition covers
them and says so in its metadata.

Some commands print what another command prints. Nothing in the text says
which of them wrote it, so they share a definition rather than having one
each, and the command they are listed under is the one the definition is
named for. These are read as well:

`b2sum` (as `sha512sum`), `gb2sum` (as `sha512sum`), `gcksum` (as `cksum`), `gdate` (as `date`), `gdf` (as `df`), `gdu` (as `du`), `genv` (as `env`), `getent` (as `etc`), `gid` (as `id`), `gls` (as `ls`), `gmd5sum` (as `md5sum`), `gsha1sum` (as `sha1sum`), `gsha224sum` (as `sha224sum`), `gsha256sum` (as `sha256sum`), `gsha384sum` (as `sha384sum`), `gsha512sum` (as `sha512sum`), `gstat` (as `stat`), `gtar` (as `tar`), `guname` (as `uname`), `gwc` (as `wc`), `gwho` (as `who`), `nerdctl` (as `docker`), `ping6` (as `ping`), `pip3` (as `pip`), `podman` (as `docker`), `printenv` (as `env`), `python` (as `pip`), `python3` (as `pip`), `systemd-resolve` (as `resolvectl`), `uv` (as `pip`), `vdir` (as `ls`).

`jz run`, `--parser` and `jz list` all take the other name.

## How a definition is chosen

Every definition carries a signature: the shape its output has.

```yaml
detect:
  os: [linux]
  args: {any: ["-h"], none: ["-i"]}
  signature:
    all: ['^Filesystem\s+Size\s+Used\s+Avail\s+Use%\s+Mounted on\s*$']
```

The signature is a necessary condition. The operating system and the
arguments are known only when jz ran the command itself; they can then
remove candidates, never promote one. Exactly one survivor is a success.
Zero and more than one are errors that say what to pass.

Some formats are not evidence of anything. A number, a tab and a path
describes `du` output and `git diff --numstat` alike, and three numbers
in a row describe almost any table. Such a definition sets
`auto_detect: false`: it stays out of automatic detection and is used
when you name it, where its signature is still checked.

## What jz will not invent

A rounded, human-readable number reaches JSON exactly as printed. The
base behind a suffix is not in the output (`df -h` steps by 1024, `df -H`
by 1000) and the value is rounded either way, so a byte count would be
two guesses stacked. Ask the command for exact numbers when you need
them: `df`, `free`, `lsblk -b`.

Where the exact and the rounded form of a command are separate
definitions, the exact one gives numbers: `df` does, `df -h` does not.
That split needs the two forms to be told apart, and telling them apart
means reading the values, which jz only sees for the first 200 lines. A
listing has no length limit, so `ls -l` and `ls -lh` are one definition
and its size is a string in both. The rule is the same one; what changes
is whether jz can know which form it has.

The same reasoning applies to `env`: a value containing a newline cannot
be told from two variables in the line-based output, so `env -0 | jz`
exists for the cases where that matters.

`NAME=value` is also what a `.env` file, a properties file and a shell
fragment look like, so the line-based form is one of the definitions jz
will not claim on sight. `env | jz --parser env` and `jz run env` read
it; `env -0` is distinctive and needs no name.
