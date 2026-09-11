---
title: Parsers
description: The commands jsonize reads today, and how it decides which definition applies.
toc: true
---

`jz list` prints the table below from the binary you have. `jz list df`
shows the variants of one command and `jz list df gnu` everything about
one definition, including where it came from and how it detects its
input. Each variant below links to the JSON Schema of what it produces;
[what each definition produces](#what-each-definition-produces) says what
those promise.

A handful of entries describe a shape rather than a command: `table`
(`whitespace`, `aligned`, `box`), `csv` (`comma`, `tab`), `kv` (`colon`,
`equals`) and `ini` (`default`). They are never chosen on their own —
two words above two words says nothing about what produced them — and
are reached by naming both halves:

```console
$ sqlite3 -box app.db 'select * from users' | jz --parser table --variant box
$ jz --parser ini --file /etc/NetworkManager/NetworkManager.conf
```

They convert nothing: a shape says nothing about what its columns mean,
so every value is the text it was cut from. Use `--define` where you want
the same reading with settings of your own.

## Supported commands

| Command | Variants |
|---------|----------|
| `amixer` | [`contents`](../schemas/amixer/contents.json), [`simple-controls`](../schemas/amixer/simple-controls.json) |
| `aplay` | [`devices`](../schemas/aplay/devices.json) |
| `apt` | [`list`](../schemas/apt/list.json), [`show`](../schemas/apt/show.json) |
| `apt-cache` | [`depends`](../schemas/apt-cache/depends.json), [`madison`](../schemas/apt-cache/madison.json), [`policy`](../schemas/apt-cache/policy.json), [`stats`](../schemas/apt-cache/stats.json) |
| `ar` | [`table-verbose`](../schemas/ar/table-verbose.json) |
| `arp` | [`alternate`](../schemas/arp/alternate.json) |
| `blkid` | [`export`](../schemas/blkid/export.json), [`linux`](../schemas/blkid/linux.json) |
| `bluetoothctl` | [`show`](../schemas/bluetoothctl/show.json) |
| `bridge` | [`fdb`](../schemas/bridge/fdb.json), [`link`](../schemas/bridge/link.json) |
| `busctl` | [`list`](../schemas/busctl/list.json) |
| `capsh` | [`print`](../schemas/capsh/print.json) |
| `cargo` | [`command-list`](../schemas/cargo/command-list.json), [`install-list`](../schemas/cargo/install-list.json) |
| `chage` | [`linux`](../schemas/chage/linux.json) |
| `chrt` | [`policy`](../schemas/chrt/policy.json) |
| `cksum` | [`posix`](../schemas/cksum/posix.json) |
| `csv` | [`comma`](../schemas/csv/comma.json), [`tab`](../schemas/csv/tab.json) |
| `curl` | [`version`](../schemas/curl/version.json) |
| `date` | [`posix`](../schemas/date/posix.json), [`rfc-email`](../schemas/date/rfc-email.json) |
| `debconf-show` | [`linux`](../schemas/debconf-show/linux.json) |
| `df` | [`bsd`](../schemas/df/bsd.json), [`bsd-human`](../schemas/df/bsd-human.json), [`busybox-human`](../schemas/df/busybox-human.json), [`gnu`](../schemas/df/gnu.json), [`gnu-human`](../schemas/df/gnu-human.json), [`gnu-inodes`](../schemas/df/gnu-inodes.json), [`gnu-inodes-human`](../schemas/df/gnu-inodes-human.json), [`gnu-type`](../schemas/df/gnu-type.json), [`gnu-type-human`](../schemas/df/gnu-type-human.json), [`portable`](../schemas/df/portable.json), [`portable-type`](../schemas/df/portable-type.json) |
| `dig` | [`bind`](../schemas/dig/bind.json) |
| `docker` | [`images-disk-usage`](../schemas/docker/images-disk-usage.json), [`images-repo-tag`](../schemas/docker/images-repo-tag.json), [`network-ls`](../schemas/docker/network-ls.json), [`ps`](../schemas/docker/ps.json), [`stats`](../schemas/docker/stats.json), [`volume-ls`](../schemas/docker/volume-ls.json) |
| `dpkg` | [`list`](../schemas/dpkg/list.json), [`selections`](../schemas/dpkg/selections.json), [`status`](../schemas/dpkg/status.json) |
| `du` | [`gnu-human`](../schemas/du/gnu-human.json), [`posix`](../schemas/du/posix.json) |
| `efibootmgr` | [`linux`](../schemas/efibootmgr/linux.json) |
| `env` | [`null-separated`](../schemas/env/null-separated.json), [`posix`](../schemas/env/posix.json) |
| `etc` | [`crontab`](../schemas/etc/crontab.json), [`fstab`](../schemas/etc/fstab.json), [`group`](../schemas/etc/group.json), [`hosts`](../schemas/etc/hosts.json), [`nsswitch`](../schemas/etc/nsswitch.json), [`os-release`](../schemas/etc/os-release.json), [`passwd`](../schemas/etc/passwd.json), [`protocols`](../schemas/etc/protocols.json), [`resolv-conf`](../schemas/etc/resolv-conf.json), [`services`](../schemas/etc/services.json) |
| `ethtool` | [`driver-info`](../schemas/ethtool/driver-info.json), [`features`](../schemas/ethtool/features.json), [`settings`](../schemas/ethtool/settings.json), [`statistics`](../schemas/ethtool/statistics.json) |
| `factor` | [`default`](../schemas/factor/default.json) |
| `fc-list` | [`posix`](../schemas/fc-list/posix.json) |
| `fdisk` | [`linux`](../schemas/fdisk/linux.json) |
| `file` | [`mime`](../schemas/file/mime.json), [`posix`](../schemas/file/posix.json) |
| `findmnt` | [`df`](../schemas/findmnt/df.json), [`linux`](../schemas/findmnt/linux.json), [`source-first`](../schemas/findmnt/source-first.json) |
| `free` | [`gnu`](../schemas/free/gnu.json), [`gnu-human`](../schemas/free/gnu-human.json), [`gnu-wide`](../schemas/free/gnu-wide.json) |
| `getcap` | [`paths`](../schemas/getcap/paths.json) |
| `getconf` | [`glibc`](../schemas/getconf/glibc.json) |
| `getfacl` | [`posix`](../schemas/getfacl/posix.json) |
| `gh` | [`auth-status`](../schemas/gh/auth-status.json) |
| `git` | [`branch-verbose`](../schemas/git/branch-verbose.json), [`config-list`](../schemas/git/config-list.json), [`count-objects`](../schemas/git/count-objects.json), [`diff-numstat`](../schemas/git/diff-numstat.json), [`diff-stat`](../schemas/git/diff-stat.json), [`for-each-ref`](../schemas/git/for-each-ref.json), [`log`](../schemas/git/log.json), [`log-oneline`](../schemas/git/log-oneline.json), [`ls-files-stage`](../schemas/git/ls-files-stage.json), [`ls-tree`](../schemas/git/ls-tree.json), [`remote-verbose`](../schemas/git/remote-verbose.json), [`shortlog-summary`](../schemas/git/shortlog-summary.json), [`show-ref`](../schemas/git/show-ref.json), [`stash-list`](../schemas/git/stash-list.json), [`status-porcelain`](../schemas/git/status-porcelain.json), [`worktree-list`](../schemas/git/worktree-list.json) |
| `go` | [`dist-list`](../schemas/go/dist-list.json), [`env`](../schemas/go/env.json), [`list-modules`](../schemas/go/list-modules.json), [`mod-graph`](../schemas/go/mod-graph.json), [`version-modules`](../schemas/go/version-modules.json) |
| `gpg` | [`colons`](../schemas/gpg/colons.json), [`version`](../schemas/gpg/version.json) |
| `hciconfig` | [`linux`](../schemas/hciconfig/linux.json) |
| `hexdump` | [`canonical`](../schemas/hexdump/canonical.json) |
| `host` | [`bind`](../schemas/host/bind.json) |
| `hostnamectl` | [`linux`](../schemas/hostnamectl/linux.json) |
| `iconv` | [`list`](../schemas/iconv/list.json) |
| `id` | [`posix`](../schemas/id/posix.json) |
| `ifconfig` | [`busybox`](../schemas/ifconfig/busybox.json) |
| `ini` | [`default`](../schemas/ini/default.json) |
| `ionice` | [`class`](../schemas/ionice/class.json) |
| `iostat` | [`cpu`](../schemas/iostat/cpu.json), [`device`](../schemas/iostat/device.json), [`extended`](../schemas/iostat/extended.json), [`human`](../schemas/iostat/human.json), [`linux`](../schemas/iostat/linux.json) |
| `ip` | [`address`](../schemas/ip/address.json), [`brief-address`](../schemas/ip/brief-address.json), [`brief-link`](../schemas/ip/brief-link.json), [`link`](../schemas/ip/link.json), [`multicast-address`](../schemas/ip/multicast-address.json), [`neighbour`](../schemas/ip/neighbour.json), [`oneline-address`](../schemas/ip/oneline-address.json), [`oneline-link`](../schemas/ip/oneline-link.json), [`route`](../schemas/ip/route.json), [`rule`](../schemas/ip/rule.json), [`stats-link`](../schemas/ip/stats-link.json), [`stats-link-detail`](../schemas/ip/stats-link-detail.json) |
| `ipcs` | [`linux`](../schemas/ipcs/linux.json), [`message-queues`](../schemas/ipcs/message-queues.json), [`semaphores`](../schemas/ipcs/semaphores.json), [`shared-memory`](../schemas/ipcs/shared-memory.json) |
| `iw` | [`dev`](../schemas/iw/dev.json), [`link`](../schemas/iw/link.json) |
| `journalctl` | [`boots`](../schemas/journalctl/boots.json), [`short`](../schemas/journalctl/short.json), [`short-iso`](../schemas/journalctl/short-iso.json), [`short-monotonic`](../schemas/journalctl/short-monotonic.json), [`short-precise`](../schemas/journalctl/short-precise.json) |
| `kv` | [`colon`](../schemas/kv/colon.json), [`equals`](../schemas/kv/equals.json) |
| `last` | [`busybox`](../schemas/last/busybox.json) |
| `ldconfig` | [`cache`](../schemas/ldconfig/cache.json) |
| `ldd` | [`posix`](../schemas/ldd/posix.json) |
| `locale` | [`keywords`](../schemas/locale/keywords.json), [`posix`](../schemas/locale/posix.json) |
| `localectl` | [`linux`](../schemas/localectl/linux.json) |
| `loginctl` | [`seats`](../schemas/loginctl/seats.json), [`sessions`](../schemas/loginctl/sessions.json), [`users`](../schemas/loginctl/users.json) |
| `losetup` | [`associations`](../schemas/losetup/associations.json), [`linux`](../schemas/losetup/linux.json) |
| `ls` | [`full-time`](../schemas/ls/full-time.json), [`long`](../schemas/ls/long.json), [`long-context`](../schemas/ls/long-context.json), [`long-inode`](../schemas/ls/long-inode.json), [`long-iso`](../schemas/ls/long-iso.json), [`long-no-owner-group`](../schemas/ls/long-no-owner-group.json), [`long-recursive`](../schemas/ls/long-recursive.json) |
| `lsattr` | [`linux`](../schemas/lsattr/linux.json) |
| `lsb_release` | [`linux`](../schemas/lsb_release/linux.json) |
| `lsblk` | [`bytes`](../schemas/lsblk/bytes.json), [`filesystems`](../schemas/lsblk/filesystems.json), [`linux`](../schemas/lsblk/linux.json), [`pairs`](../schemas/lsblk/pairs.json), [`topology`](../schemas/lsblk/topology.json) |
| `lsclocks` | [`linux`](../schemas/lsclocks/linux.json) |
| `lscpu` | [`caches`](../schemas/lscpu/caches.json), [`extended`](../schemas/lscpu/extended.json), [`linux`](../schemas/lscpu/linux.json) |
| `lsfd` | [`linux`](../schemas/lsfd/linux.json) |
| `lsipc` | [`linux`](../schemas/lsipc/linux.json) |
| `lsirq` | [`linux`](../schemas/lsirq/linux.json) |
| `lslocks` | [`linux`](../schemas/lslocks/linux.json) |
| `lslogins` | [`linux`](../schemas/lslogins/linux.json) |
| `lsmem` | [`linux`](../schemas/lsmem/linux.json) |
| `lsmod` | [`busybox`](../schemas/lsmod/busybox.json), [`linux`](../schemas/lsmod/linux.json) |
| `lsns` | [`linux`](../schemas/lsns/linux.json) |
| `lsof` | [`linux`](../schemas/lsof/linux.json), [`tasks`](../schemas/lsof/tasks.json) |
| `lspci` | [`kernel`](../schemas/lspci/kernel.json), [`linux`](../schemas/lspci/linux.json), [`machine`](../schemas/lspci/machine.json), [`numeric`](../schemas/lspci/numeric.json), [`numeric-names`](../schemas/lspci/numeric-names.json), [`verbose`](../schemas/lspci/verbose.json), [`verbose-machine`](../schemas/lspci/verbose-machine.json) |
| `lsusb` | [`linux`](../schemas/lsusb/linux.json), [`tree`](../schemas/lsusb/tree.json), [`verbose`](../schemas/lsusb/verbose.json) |
| `md5sum` | [`posix`](../schemas/md5sum/posix.json) |
| `mise` | [`ls`](../schemas/mise/ls.json) |
| `modinfo` | [`linux`](../schemas/modinfo/linux.json) |
| `mokutil` | [`sbat`](../schemas/mokutil/sbat.json) |
| `mount` | [`bsd`](../schemas/mount/bsd.json), [`linux`](../schemas/mount/linux.json) |
| `mpstat` | [`interrupts`](../schemas/mpstat/interrupts.json), [`linux`](../schemas/mpstat/linux.json) |
| `mtr` | [`report`](../schemas/mtr/report.json) |
| `namei` | [`long`](../schemas/namei/long.json) |
| `netstat` | [`all-sockets`](../schemas/netstat/all-sockets.json), [`internet`](../schemas/netstat/internet.json), [`routing`](../schemas/netstat/routing.json), [`unix`](../schemas/netstat/unix.json) |
| `networkctl` | [`list`](../schemas/networkctl/list.json) |
| `nm` | [`dynamic`](../schemas/nm/dynamic.json) |
| `nmcli` | [`connection`](../schemas/nmcli/connection.json), [`device`](../schemas/nmcli/device.json), [`device-show`](../schemas/nmcli/device-show.json), [`general`](../schemas/nmcli/general.json), [`wifi`](../schemas/nmcli/wifi.json) |
| `npm` | [`ls`](../schemas/npm/ls.json), [`outdated`](../schemas/npm/outdated.json) |
| `nslookup` | [`query`](../schemas/nslookup/query.json) |
| `nstat` | [`counters`](../schemas/nstat/counters.json) |
| `numactl` | [`hardware`](../schemas/numactl/hardware.json) |
| `objdump` | [`section-headers`](../schemas/objdump/section-headers.json) |
| `od` | [`hex-bytes`](../schemas/od/hex-bytes.json) |
| `openssl` | [`ciphers`](../schemas/openssl/ciphers.json), [`ciphers-codes`](../schemas/openssl/ciphers-codes.json), [`version-all`](../schemas/openssl/version-all.json) |
| `pactl` | [`info`](../schemas/pactl/info.json), [`short-cards`](../schemas/pactl/short-cards.json), [`short-clients`](../schemas/pactl/short-clients.json), [`short-devices`](../schemas/pactl/short-devices.json), [`sinks`](../schemas/pactl/sinks.json) |
| `parted` | [`machine`](../schemas/parted/machine.json) |
| `pidstat` | [`io`](../schemas/pidstat/io.json), [`linux`](../schemas/pidstat/linux.json), [`memory`](../schemas/pidstat/memory.json), [`switches`](../schemas/pidstat/switches.json) |
| `ping` | [`linux`](../schemas/ping/linux.json) |
| `pip` | [`columns`](../schemas/pip/columns.json) |
| `pmap` | [`linux`](../schemas/pmap/linux.json) |
| `powerprofilesctl` | [`list`](../schemas/powerprofilesctl/list.json) |
| `prlimit` | [`linux`](../schemas/prlimit/linux.json) |
| `proc` | [`buddyinfo`](../schemas/proc/buddyinfo.json), [`consoles`](../schemas/proc/consoles.json), [`cpuinfo-x86`](../schemas/proc/cpuinfo-x86.json), [`crypto`](../schemas/proc/crypto.json), [`devices`](../schemas/proc/devices.json), [`diskstats`](../schemas/proc/diskstats.json), [`filesystems`](../schemas/proc/filesystems.json), [`interrupts`](../schemas/proc/interrupts.json), [`loadavg`](../schemas/proc/loadavg.json), [`meminfo`](../schemas/proc/meminfo.json), [`modules`](../schemas/proc/modules.json), [`mountinfo`](../schemas/proc/mountinfo.json), [`net-arp`](../schemas/proc/net-arp.json), [`net-dev`](../schemas/proc/net-dev.json), [`net-route`](../schemas/proc/net-route.json), [`net-unix`](../schemas/proc/net-unix.json), [`partitions`](../schemas/proc/partitions.json), [`schedstat`](../schemas/proc/schedstat.json), [`self-limits`](../schemas/proc/self-limits.json), [`self-status`](../schemas/proc/self-status.json), [`softirqs`](../schemas/proc/softirqs.json), [`stat`](../schemas/proc/stat.json), [`uptime`](../schemas/proc/uptime.json), [`vmstat`](../schemas/proc/vmstat.json) |
| `ps` | [`bsd`](../schemas/ps/bsd.json), [`busybox`](../schemas/ps/busybox.json), [`jobs`](../schemas/ps/jobs.json), [`posix`](../schemas/ps/posix.json), [`threads`](../schemas/ps/threads.json), [`unix`](../schemas/ps/unix.json) |
| `readelf` | [`header`](../schemas/readelf/header.json) |
| `resolvectl` | [`status`](../schemas/resolvectl/status.json) |
| `rfkill` | [`linux`](../schemas/rfkill/linux.json), [`list`](../schemas/rfkill/list.json) |
| `route` | [`linux`](../schemas/route/linux.json) |
| `rsync` | [`itemize`](../schemas/rsync/itemize.json) |
| `rustup` | [`component-list`](../schemas/rustup/component-list.json), [`target-list`](../schemas/rustup/target-list.json), [`toolchains`](../schemas/rustup/toolchains.json), [`toolchains-verbose`](../schemas/rustup/toolchains-verbose.json) |
| `sar` | [`cpu`](../schemas/sar/cpu.json), [`filesystem`](../schemas/sar/filesystem.json), [`io`](../schemas/sar/io.json), [`memory`](../schemas/sar/memory.json), [`network-device`](../schemas/sar/network-device.json), [`paging`](../schemas/sar/paging.json), [`queue`](../schemas/sar/queue.json), [`swap`](../schemas/sar/swap.json), [`task`](../schemas/sar/task.json) |
| `scc` | [`default`](../schemas/scc/default.json) |
| `sensors` | [`linux`](../schemas/sensors/linux.json), [`raw`](../schemas/sensors/raw.json) |
| `service` | [`status-all`](../schemas/service/status-all.json) |
| `sfdisk` | [`dump`](../schemas/sfdisk/dump.json) |
| `sha1sum` | [`posix`](../schemas/sha1sum/posix.json) |
| `sha224sum` | [`posix`](../schemas/sha224sum/posix.json) |
| `sha256sum` | [`posix`](../schemas/sha256sum/posix.json) |
| `sha384sum` | [`posix`](../schemas/sha384sum/posix.json) |
| `sha512sum` | [`posix`](../schemas/sha512sum/posix.json) |
| `size` | [`gnu`](../schemas/size/gnu.json) |
| `smartctl` | [`scan`](../schemas/smartctl/scan.json) |
| `snap` | [`list`](../schemas/snap/list.json) |
| `ss` | [`linux`](../schemas/ss/linux.json), [`single-protocol`](../schemas/ss/single-protocol.json), [`summary`](../schemas/ss/summary.json) |
| `ssh-keygen` | [`fingerprint`](../schemas/ssh-keygen/fingerprint.json) |
| `stat` | [`gnu`](../schemas/stat/gnu.json), [`gnu-filesystem`](../schemas/stat/gnu-filesystem.json), [`gnu-terse`](../schemas/stat/gnu-terse.json) |
| `swapon` | [`legacy`](../schemas/swapon/legacy.json), [`linux`](../schemas/swapon/linux.json) |
| `sysctl` | [`linux`](../schemas/sysctl/linux.json) |
| `systemctl` | [`dependencies`](../schemas/systemctl/dependencies.json), [`jobs`](../schemas/systemctl/jobs.json), [`show`](../schemas/systemctl/show.json), [`show-properties`](../schemas/systemctl/show-properties.json), [`sockets`](../schemas/systemctl/sockets.json), [`status`](../schemas/systemctl/status.json), [`timers`](../schemas/systemctl/timers.json), [`unit-files`](../schemas/systemctl/unit-files.json), [`units`](../schemas/systemctl/units.json), [`units-jobs`](../schemas/systemctl/units-jobs.json) |
| `systemd-analyze` | [`blame`](../schemas/systemd-analyze/blame.json), [`critical-chain`](../schemas/systemd-analyze/critical-chain.json), [`security`](../schemas/systemd-analyze/security.json), [`time`](../schemas/systemd-analyze/time.json) |
| `systemd-cgtop` | [`batch`](../schemas/systemd-cgtop/batch.json) |
| `systemd-id128` | [`show`](../schemas/systemd-id128/show.json) |
| `systemd-inhibit` | [`list`](../schemas/systemd-inhibit/list.json) |
| `systemd-path` | [`paths`](../schemas/systemd-path/paths.json) |
| `table` | [`aligned`](../schemas/table/aligned.json), [`box`](../schemas/table/box.json), [`whitespace`](../schemas/table/whitespace.json) |
| `tar` | [`busybox`](../schemas/tar/busybox.json), [`gnu`](../schemas/tar/gnu.json) |
| `taskset` | [`affinity-list`](../schemas/taskset/affinity-list.json) |
| `tc` | [`qdisc`](../schemas/tc/qdisc.json), [`qdisc-stats`](../schemas/tc/qdisc-stats.json) |
| `timedatectl` | [`linux`](../schemas/timedatectl/linux.json), [`timesync`](../schemas/timedatectl/timesync.json), [`timezones`](../schemas/timedatectl/timezones.json) |
| `top` | [`linux`](../schemas/top/linux.json) |
| `tracepath` | [`linux`](../schemas/tracepath/linux.json) |
| `tree` | [`listing`](../schemas/tree/listing.json) |
| `udevadm` | [`info`](../schemas/udevadm/info.json) |
| `ulimit` | [`bash`](../schemas/ulimit/bash.json), [`dash`](../schemas/ulimit/dash.json) |
| `uname` | [`darwin`](../schemas/uname/darwin.json), [`linux`](../schemas/uname/linux.json) |
| `unzip` | [`busybox`](../schemas/unzip/busybox.json), [`info-zip`](../schemas/unzip/info-zip.json) |
| `update-alternatives` | [`query`](../schemas/update-alternatives/query.json), [`selections`](../schemas/update-alternatives/selections.json) |
| `upower` | [`device`](../schemas/upower/device.json), [`dump`](../schemas/upower/dump.json), [`enumerate`](../schemas/upower/enumerate.json) |
| `uptime` | [`bsd`](../schemas/uptime/bsd.json), [`linux`](../schemas/uptime/linux.json), [`pretty`](../schemas/uptime/pretty.json), [`since`](../schemas/uptime/since.json) |
| `uuidparse` | [`linux`](../schemas/uuidparse/linux.json) |
| `uv` | [`pip-show`](../schemas/uv/pip-show.json), [`tool-list`](../schemas/uv/tool-list.json) |
| `vmstat` | [`linux`](../schemas/vmstat/linux.json), [`linux-active`](../schemas/vmstat/linux-active.json), [`linux-disk`](../schemas/vmstat/linux-disk.json), [`linux-disk-summary`](../schemas/vmstat/linux-disk-summary.json), [`linux-stats`](../schemas/vmstat/linux-stats.json) |
| `w` | [`bsd`](../schemas/w/bsd.json), [`linux`](../schemas/w/linux.json), [`linux-short`](../schemas/w/linux-short.json) |
| `wc` | [`posix`](../schemas/wc/posix.json) |
| `who` | [`iso`](../schemas/who/iso.json), [`posix`](../schemas/who/posix.json) |
| `xrandr` | [`linux`](../schemas/xrandr/linux.json), [`listmonitors`](../schemas/xrandr/listmonitors.json) |
| `xxd` | [`default`](../schemas/xxd/default.json) |
| `zipinfo` | [`default`](../schemas/zipinfo/default.json) |

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

## What each definition produces

Every definition has a JSON Schema of its output, derived from the
definition rather than written beside it: the keys are its columns,
groups and parts, the types are its field conversions, a key is required
when every object carries it, and a value may be null where the engine can
leave it empty (an aligned cell with nothing under it, a group that did not
take part, a value `null_if` names). A group that can only match a few
literals is an `enum`. Where the keys come from the input, as in a table
that names its columns from its header, the schema says what the values
look like instead of naming the keys.

```console
$ jz list --schema df gnu
$ jz list --schema df gnu | jq '.items.required'
```

The schemas are published under
`https://nao1215.github.io/jsonize/schemas/COMMAND/VARIANT.json`, the
`$id` of each, and every fixture in the registry is checked against its
own on every change, by jz and by an independent validator.

`x-jsonize.version` is the version of that output contract. It is not
`format`, which versions how a definition is written; the two change for
unrelated reasons. A change a program reading the output could be broken
by — a key removed, a type changed or made nullable, an object become an
array, a key that is no longer always there or that now always is, a
value added to or taken from an `enum` — is refused unless the version
goes up with it. Adding a key that may appear is the one change that
keeps the version. The JSON jz prints carries no version: the definition
it was read with, which `--explain` names, identifies the contract.

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
