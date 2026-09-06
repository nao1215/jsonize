---
title: Parsers
description: The commands jsonize reads today, and how it decides which definition applies.
---

`jz list` prints the table below from the binary you have. `jz list df`
shows the variants of one command and `jz list df gnu` everything about
one definition, including where it came from and how it detects its
input.

## Supported commands

| Command | Variants |
|---------|----------|
| `blkid` | `linux` |
| `df` | `gnu`, `gnu-human`, `bsd`, `bsd-human`, `busybox-human` |
| `du` | `posix` |
| `env` | `posix`, `null-separated` |
| `file` | `posix` |
| `findmnt` | `linux` |
| `free` | `gnu`, `gnu-human`, `gnu-wide` |
| `group` | `posix` |
| `host` | `bind` |
| `hosts` | `posix` |
| `id` | `posix` |
| `ip` | `route`, `brief-address` |
| `ls` | `long` |
| `lsattr` | `linux` |
| `lsb_release` | `linux` |
| `lsblk` | `linux` |
| `lsmod` | `linux` |
| `lspci` | `linux` |
| `lsusb` | `linux` |
| `mount` | `linux`, `bsd` |
| `os-release` | `linux` |
| `passwd` | `posix` |
| `ping` | `linux` |
| `ps` | `unix`, `bsd`, `busybox` |
| `ss` | `linux` |
| `stat` | `gnu` |
| `swapon` | `linux` |
| `sysctl` | `linux` |
| `systemctl` | `units` |
| `timedatectl` | `linux` |
| `uname` | `linux`, `darwin` |
| `uptime` | `linux`, `bsd` |
| `vmstat` | `linux` |
| `w` | `linux`, `bsd` |
| `wc` | `posix` |

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
