---
title: Parsers
description: The commands jsonize reads today, and how it decides which definition applies.
toc: true
---

`jz list` prints the table below from the binary you have. `jz list df`
shows the variants of one command and `jz list df gnu` everything about
one definition, including where it came from and how it detects its
input.

## Supported commands

| Command | Variants |
|---------|----------|
| `apt` | `list` |
| `blkid` | `linux` |
| `bluetoothctl` | `show` |
| `cargo` | `command-list` |
| `cksum` | `posix` |
| `df` | `bsd`, `bsd-human`, `busybox-human`, `gnu`, `gnu-human`, `gnu-inodes`, `gnu-inodes-human`, `gnu-type`, `gnu-type-human` |
| `dig` | `bind` |
| `dpkg` | `list` |
| `du` | `gnu-human`, `posix` |
| `env` | `null-separated`, `posix` |
| `etc` | `group`, `hosts`, `os-release`, `passwd` |
| `ethtool` | `driver-info` |
| `fdisk` | `linux` |
| `file` | `posix` |
| `findmnt` | `df`, `linux`, `source-first` |
| `free` | `gnu`, `gnu-human`, `gnu-wide` |
| `getconf` | `glibc` |
| `git` | `branch-verbose`, `config-list`, `log-oneline`, `remote-verbose`, `stash-list`, `status-porcelain` |
| `go` | `env` |
| `host` | `bind` |
| `hostnamectl` | `linux` |
| `id` | `posix` |
| `iostat` | `cpu`, `device`, `linux` |
| `ip` | `brief-address`, `brief-link`, `neighbour`, `route`, `stats-link` |
| `iw` | `link` |
| `journalctl` | `boots`, `short`, `short-iso`, `short-precise` |
| `locale` | `posix` |
| `localectl` | `linux` |
| `loginctl` | `seats`, `sessions`, `users` |
| `losetup` | `linux` |
| `ls` | `long`, `long-context`, `long-inode`, `long-no-owner-group` |
| `lsattr` | `linux` |
| `lsb_release` | `linux` |
| `lsblk` | `bytes`, `filesystems`, `linux`, `pairs`, `topology` |
| `lscpu` | `linux` |
| `lsipc` | `linux` |
| `lslocks` | `linux` |
| `lslogins` | `linux` |
| `lsmem` | `linux` |
| `lsmod` | `busybox`, `linux` |
| `lsns` | `linux` |
| `lsof` | `linux` |
| `lspci` | `kernel`, `linux`, `machine`, `numeric`, `numeric-names`, `verbose-machine` |
| `lsusb` | `linux` |
| `md5sum` | `posix` |
| `mise` | `ls` |
| `mount` | `bsd`, `linux` |
| `mpstat` | `linux` |
| `nmcli` | `connection`, `device` |
| `npm` | `ls`, `outdated` |
| `parted` | `machine` |
| `pidstat` | `linux` |
| `ping` | `linux` |
| `pip` | `columns` |
| `ps` | `bsd`, `busybox`, `posix`, `unix` |
| `rfkill` | `linux`, `list` |
| `sensors` | `linux` |
| `sfdisk` | `dump` |
| `sha256sum` | `posix` |
| `ss` | `linux` |
| `stat` | `gnu`, `gnu-filesystem`, `gnu-terse` |
| `swapon` | `legacy`, `linux` |
| `sysctl` | `linux` |
| `systemctl` | `show`, `sockets`, `timers`, `unit-files`, `units` |
| `systemd-analyze` | `blame` |
| `tar` | `busybox`, `gnu` |
| `timedatectl` | `linux`, `timesync`, `timezones` |
| `top` | `linux` |
| `tracepath` | `linux` |
| `ulimit` | `bash`, `dash` |
| `uname` | `darwin`, `linux` |
| `unzip` | `busybox`, `info-zip` |
| `upower` | `device`, `dump`, `enumerate` |
| `uptime` | `bsd`, `linux` |
| `vmstat` | `linux`, `linux-active`, `linux-disk`, `linux-disk-summary`, `linux-stats` |
| `w` | `bsd`, `linux`, `linux-short` |
| `wc` | `posix` |
| `who` | `posix` |
| `xrandr` | `linux`, `listmonitors` |
| `zipinfo` | `default` |

A variant is one output format of a command. GNU `df`, `df -h`, macOS
`df` and BusyBox `df -h` are four formats, so they are four definitions.
When several implementations print the same format, one definition covers
them and says so in its metadata.

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
them: `df`, `free`, `ls -l`, `lsblk -b`.

The same reasoning applies to `env`: a value containing a newline cannot
be told from two variables in the line-based output, so `env -0 | jz`
exists for the cases where that matters.

`NAME=value` is also what a `.env` file, a properties file and a shell
fragment look like, so the line-based form is one of the definitions jz
will not claim on sight. `env | jz --parser env` and `jz run env` read
it; `env -0` is distinctive and needs no name.
