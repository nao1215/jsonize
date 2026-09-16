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
| `7z` | [`list`](../schemas/7z/list.json), [`list-technical`](../schemas/7z/list-technical.json) |
| `actionlint` | [`default`](../schemas/actionlint/default.json) |
| `amixer` | [`contents`](../schemas/amixer/contents.json), [`simple-controls`](../schemas/amixer/simple-controls.json) |
| `aplay` | [`devices`](../schemas/aplay/devices.json) |
| `apt` | [`list`](../schemas/apt/list.json), [`show`](../schemas/apt/show.json) |
| `apt-cache` | [`depends`](../schemas/apt-cache/depends.json), [`madison`](../schemas/apt-cache/madison.json), [`policy`](../schemas/apt-cache/policy.json), [`search`](../schemas/apt-cache/search.json), [`show`](../schemas/apt-cache/show.json), [`stats`](../schemas/apt-cache/stats.json) |
| `apt-config` | [`dump`](../schemas/apt-config/dump.json) |
| `ar` | [`table-verbose`](../schemas/ar/table-verbose.json) |
| `arp` | [`alternate`](../schemas/arp/alternate.json), [`darwin`](../schemas/arp/darwin.json), [`freebsd`](../schemas/arp/freebsd.json), [`net-tools`](../schemas/arp/net-tools.json), [`windows`](../schemas/arp/windows.json) |
| `avahi-browse` | [`parsable`](../schemas/avahi-browse/parsable.json) |
| `bat` | [`list-languages`](../schemas/bat/list-languages.json) |
| `blkid` | [`export`](../schemas/blkid/export.json), [`linux`](../schemas/blkid/linux.json) |
| `bluetoothctl` | [`show`](../schemas/bluetoothctl/show.json) |
| `brew` | [`outdated`](../schemas/brew/outdated.json) |
| `bridge` | [`fdb`](../schemas/bridge/fdb.json), [`link`](../schemas/bridge/link.json) |
| `busctl` | [`list`](../schemas/busctl/list.json), [`tree`](../schemas/busctl/tree.json), [`tree-services`](../schemas/busctl/tree-services.json) |
| `capsh` | [`print`](../schemas/capsh/print.json) |
| `cargo` | [`command-list`](../schemas/cargo/command-list.json), [`install-list`](../schemas/cargo/install-list.json), [`search`](../schemas/cargo/search.json), [`tree`](../schemas/cargo/tree.json), [`verbose-version`](../schemas/cargo/verbose-version.json) |
| `chage` | [`linux`](../schemas/chage/linux.json) |
| `chcp` | [`windows`](../schemas/chcp/windows.json) |
| `chrt` | [`policy`](../schemas/chrt/policy.json) |
| `cksum` | [`posix`](../schemas/cksum/posix.json) |
| `csrutil` | [`status`](../schemas/csrutil/status.json) |
| `csv` | [`comma`](../schemas/csv/comma.json), [`comma-no-header`](../schemas/csv/comma-no-header.json), [`tab`](../schemas/csv/tab.json), [`tab-no-header`](../schemas/csv/tab-no-header.json) |
| `curl` | [`headers`](../schemas/curl/headers.json), [`version`](../schemas/curl/version.json) |
| `date` | [`posix`](../schemas/date/posix.json), [`rfc-email`](../schemas/date/rfc-email.json) |
| `debconf-show` | [`linux`](../schemas/debconf-show/linux.json) |
| `df` | [`bsd`](../schemas/df/bsd.json), [`bsd-human`](../schemas/df/bsd-human.json), [`busybox-human`](../schemas/df/busybox-human.json), [`freebsd`](../schemas/df/freebsd.json), [`freebsd-blocks`](../schemas/df/freebsd-blocks.json), [`freebsd-blocks-type`](../schemas/df/freebsd-blocks-type.json), [`freebsd-human`](../schemas/df/freebsd-human.json), [`freebsd-inodes`](../schemas/df/freebsd-inodes.json), [`freebsd-type`](../schemas/df/freebsd-type.json), [`freebsd-type-human`](../schemas/df/freebsd-type-human.json), [`gnu`](../schemas/df/gnu.json), [`gnu-blocks`](../schemas/df/gnu-blocks.json), [`gnu-blocks-type`](../schemas/df/gnu-blocks-type.json), [`gnu-human`](../schemas/df/gnu-human.json), [`gnu-inodes`](../schemas/df/gnu-inodes.json), [`gnu-inodes-human`](../schemas/df/gnu-inodes-human.json), [`gnu-type`](../schemas/df/gnu-type.json), [`gnu-type-human`](../schemas/df/gnu-type-human.json), [`portable`](../schemas/df/portable.json), [`portable-blocks`](../schemas/df/portable-blocks.json), [`portable-type`](../schemas/df/portable-type.json) |
| `dig` | [`answers`](../schemas/dig/answers.json), [`axfr`](../schemas/dig/axfr.json), [`bind`](../schemas/dig/bind.json) |
| `diskutil` | [`list`](../schemas/diskutil/list.json) |
| `docker` | [`buildx-ls`](../schemas/docker/buildx-ls.json), [`compose-ls`](../schemas/docker/compose-ls.json), [`compose-ps`](../schemas/docker/compose-ps.json), [`context-ls`](../schemas/docker/context-ls.json), [`images-disk-usage`](../schemas/docker/images-disk-usage.json), [`images-repo-tag`](../schemas/docker/images-repo-tag.json), [`network-ls`](../schemas/docker/network-ls.json), [`ps`](../schemas/docker/ps.json), [`ps-size`](../schemas/docker/ps-size.json), [`stats`](../schemas/docker/stats.json), [`system-df`](../schemas/docker/system-df.json), [`system-df-verbose`](../schemas/docker/system-df-verbose.json), [`version`](../schemas/docker/version.json), [`volume-ls`](../schemas/docker/volume-ls.json) |
| `dpkg` | [`list`](../schemas/dpkg/list.json), [`selections`](../schemas/dpkg/selections.json), [`status`](../schemas/dpkg/status.json) |
| `dpkg-deb` | [`info`](../schemas/dpkg-deb/info.json) |
| `dpkg-divert` | [`list`](../schemas/dpkg-divert/list.json) |
| `dpkg-query` | [`list`](../schemas/dpkg-query/list.json) |
| `driverquery` | [`table`](../schemas/driverquery/table.json), [`verbose`](../schemas/driverquery/verbose.json) |
| `du` | [`gnu-human`](../schemas/du/gnu-human.json), [`posix`](../schemas/du/posix.json) |
| `efibootmgr` | [`linux`](../schemas/efibootmgr/linux.json) |
| `env` | [`null-separated`](../schemas/env/null-separated.json), [`posix`](../schemas/env/posix.json) |
| `etc` | [`crontab`](../schemas/etc/crontab.json), [`fstab`](../schemas/etc/fstab.json), [`group`](../schemas/etc/group.json), [`hosts`](../schemas/etc/hosts.json), [`nsswitch`](../schemas/etc/nsswitch.json), [`os-release`](../schemas/etc/os-release.json), [`passwd`](../schemas/etc/passwd.json), [`protocols`](../schemas/etc/protocols.json), [`resolv-conf`](../schemas/etc/resolv-conf.json), [`services`](../schemas/etc/services.json) |
| `ethtool` | [`driver-info`](../schemas/ethtool/driver-info.json), [`features`](../schemas/ethtool/features.json), [`pause`](../schemas/ethtool/pause.json), [`ring`](../schemas/ethtool/ring.json), [`settings`](../schemas/ethtool/settings.json), [`statistics`](../schemas/ethtool/statistics.json) |
| `exiftool` | [`default`](../schemas/exiftool/default.json), [`groups`](../schemas/exiftool/groups.json) |
| `eza` | [`full-iso`](../schemas/eza/full-iso.json), [`long`](../schemas/eza/long.json), [`long-group`](../schemas/eza/long-group.json), [`long-iso`](../schemas/eza/long-iso.json), [`long-links`](../schemas/eza/long-links.json) |
| `factor` | [`default`](../schemas/factor/default.json) |
| `fc-list` | [`posix`](../schemas/fc-list/posix.json) |
| `fc-match` | [`default`](../schemas/fc-match/default.json) |
| `fdesetup` | [`status`](../schemas/fdesetup/status.json) |
| `fdisk` | [`linux`](../schemas/fdisk/linux.json) |
| `file` | [`mime`](../schemas/file/mime.json), [`posix`](../schemas/file/posix.json) |
| `findmnt` | [`df`](../schemas/findmnt/df.json), [`linux`](../schemas/findmnt/linux.json), [`source-first`](../schemas/findmnt/source-first.json) |
| `free` | [`gnu`](../schemas/free/gnu.json), [`gnu-human`](../schemas/free/gnu-human.json), [`gnu-wide`](../schemas/free/gnu-wide.json), [`gnu-wide-human`](../schemas/free/gnu-wide-human.json) |
| `getcap` | [`paths`](../schemas/getcap/paths.json) |
| `getconf` | [`glibc`](../schemas/getconf/glibc.json) |
| `getent` | [`ahosts`](../schemas/getent/ahosts.json) |
| `getfacl` | [`posix`](../schemas/getfacl/posix.json) |
| `getmac` | [`verbose`](../schemas/getmac/verbose.json) |
| `gh` | [`auth-status`](../schemas/gh/auth-status.json), [`issue-list`](../schemas/gh/issue-list.json), [`pr-list`](../schemas/gh/pr-list.json), [`release-list`](../schemas/gh/release-list.json), [`repo-list`](../schemas/gh/repo-list.json), [`run-list`](../schemas/gh/run-list.json), [`workflow-list`](../schemas/gh/workflow-list.json) |
| `gio` | [`mime`](../schemas/gio/mime.json) |
| `git` | [`branch`](../schemas/git/branch.json), [`branch-verbose`](../schemas/git/branch-verbose.json), [`cherry`](../schemas/git/cherry.json), [`config-list`](../schemas/git/config-list.json), [`config-list-origin`](../schemas/git/config-list-origin.json), [`count-objects`](../schemas/git/count-objects.json), [`count-objects-human`](../schemas/git/count-objects-human.json), [`diff-name-status`](../schemas/git/diff-name-status.json), [`diff-numstat`](../schemas/git/diff-numstat.json), [`diff-shortstat`](../schemas/git/diff-shortstat.json), [`diff-stat`](../schemas/git/diff-stat.json), [`for-each-ref`](../schemas/git/for-each-ref.json), [`log`](../schemas/git/log.json), [`log-name-only`](../schemas/git/log-name-only.json), [`log-name-status`](../schemas/git/log-name-status.json), [`log-numstat`](../schemas/git/log-numstat.json), [`log-oneline`](../schemas/git/log-oneline.json), [`log-stat`](../schemas/git/log-stat.json), [`ls-files-stage`](../schemas/git/ls-files-stage.json), [`ls-remote`](../schemas/git/ls-remote.json), [`ls-tree`](../schemas/git/ls-tree.json), [`ls-tree-long`](../schemas/git/ls-tree-long.json), [`reflog`](../schemas/git/reflog.json), [`remote-verbose`](../schemas/git/remote-verbose.json), [`shortlog-summary`](../schemas/git/shortlog-summary.json), [`show-ref`](../schemas/git/show-ref.json), [`stash-list`](../schemas/git/stash-list.json), [`status-porcelain`](../schemas/git/status-porcelain.json), [`status-porcelain-v2`](../schemas/git/status-porcelain-v2.json), [`status-short-branch`](../schemas/git/status-short-branch.json), [`worktree-list`](../schemas/git/worktree-list.json), [`worktree-porcelain`](../schemas/git/worktree-porcelain.json) |
| `go` | [`bench`](../schemas/go/bench.json), [`dist-list`](../schemas/go/dist-list.json), [`env`](../schemas/go/env.json), [`list-modules`](../schemas/go/list-modules.json), [`mod-graph`](../schemas/go/mod-graph.json), [`test`](../schemas/go/test.json), [`tool-cover-func`](../schemas/go/tool-cover-func.json), [`version`](../schemas/go/version.json), [`version-modules`](../schemas/go/version-modules.json), [`vet`](../schemas/go/vet.json) |
| `golangci-lint` | [`text`](../schemas/golangci-lint/text.json) |
| `gpg` | [`colons`](../schemas/gpg/colons.json), [`list-keys`](../schemas/gpg/list-keys.json), [`version`](../schemas/gpg/version.json) |
| `gpgconf` | [`list-components`](../schemas/gpgconf/list-components.json), [`list-dirs`](../schemas/gpgconf/list-dirs.json) |
| `gsettings` | [`list-recursively`](../schemas/gsettings/list-recursively.json) |
| `gzip` | [`list`](../schemas/gzip/list.json), [`list-verbose`](../schemas/gzip/list-verbose.json) |
| `hciconfig` | [`linux`](../schemas/hciconfig/linux.json) |
| `hexdump` | [`canonical`](../schemas/hexdump/canonical.json) |
| `host` | [`bind`](../schemas/host/bind.json) |
| `hostname` | [`all-addresses`](../schemas/hostname/all-addresses.json) |
| `hostnamectl` | [`linux`](../schemas/hostnamectl/linux.json) |
| `hyperfine` | [`basic`](../schemas/hyperfine/basic.json) |
| `iconv` | [`list`](../schemas/iconv/list.json) |
| `id` | [`posix`](../schemas/id/posix.json) |
| `identify` | [`default`](../schemas/identify/default.json) |
| `ifconfig` | [`bsd`](../schemas/ifconfig/bsd.json), [`busybox`](../schemas/ifconfig/busybox.json), [`net-tools`](../schemas/ifconfig/net-tools.json), [`net-tools-short`](../schemas/ifconfig/net-tools-short.json) |
| `ini` | [`default`](../schemas/ini/default.json) |
| `ionice` | [`class`](../schemas/ionice/class.json) |
| `iostat` | [`cpu`](../schemas/iostat/cpu.json), [`cpu-timestamped`](../schemas/iostat/cpu-timestamped.json), [`device`](../schemas/iostat/device.json), [`device-timestamped`](../schemas/iostat/device-timestamped.json), [`extended`](../schemas/iostat/extended.json), [`extended-device`](../schemas/iostat/extended-device.json), [`extended-timestamped`](../schemas/iostat/extended-timestamped.json), [`freebsd-extended`](../schemas/iostat/freebsd-extended.json), [`human`](../schemas/iostat/human.json), [`human-sizes`](../schemas/iostat/human-sizes.json), [`human-sizes-timestamped`](../schemas/iostat/human-sizes-timestamped.json), [`human-timestamped`](../schemas/iostat/human-timestamped.json), [`linux`](../schemas/iostat/linux.json), [`linux-timestamped`](../schemas/iostat/linux-timestamped.json) |
| `ip` | [`address`](../schemas/ip/address.json), [`brief-address`](../schemas/ip/brief-address.json), [`brief-link`](../schemas/ip/brief-link.json), [`link`](../schemas/ip/link.json), [`multicast-address`](../schemas/ip/multicast-address.json), [`neighbour`](../schemas/ip/neighbour.json), [`oneline-address`](../schemas/ip/oneline-address.json), [`oneline-link`](../schemas/ip/oneline-link.json), [`route`](../schemas/ip/route.json), [`route-get`](../schemas/ip/route-get.json), [`rule`](../schemas/ip/rule.json), [`stats-link`](../schemas/ip/stats-link.json), [`stats-link-detail`](../schemas/ip/stats-link-detail.json) |
| `ipconfig` | [`all`](../schemas/ipconfig/all.json), [`windows`](../schemas/ipconfig/windows.json) |
| `ipcs` | [`limits`](../schemas/ipcs/limits.json), [`linux`](../schemas/ipcs/linux.json), [`message-queues`](../schemas/ipcs/message-queues.json), [`semaphores`](../schemas/ipcs/semaphores.json), [`shared-memory`](../schemas/ipcs/shared-memory.json) |
| `iw` | [`dev`](../schemas/iw/dev.json), [`link`](../schemas/iw/link.json), [`reg-get`](../schemas/iw/reg-get.json) |
| `journalctl` | [`boots`](../schemas/journalctl/boots.json), [`short`](../schemas/journalctl/short.json), [`short-iso`](../schemas/journalctl/short-iso.json), [`short-monotonic`](../schemas/journalctl/short-monotonic.json), [`short-precise`](../schemas/journalctl/short-precise.json) |
| `just` | [`list`](../schemas/just/list.json) |
| `kldstat` | [`freebsd`](../schemas/kldstat/freebsd.json), [`freebsd-human`](../schemas/kldstat/freebsd-human.json) |
| `kubectl` | [`api-resources`](../schemas/kubectl/api-resources.json), [`contexts`](../schemas/kubectl/contexts.json), [`deployments`](../schemas/kubectl/deployments.json), [`namespaces`](../schemas/kubectl/namespaces.json), [`nodes`](../schemas/kubectl/nodes.json), [`pods`](../schemas/kubectl/pods.json), [`services`](../schemas/kubectl/services.json), [`version`](../schemas/kubectl/version.json) |
| `kv` | [`colon`](../schemas/kv/colon.json), [`equals`](../schemas/kv/equals.json) |
| `last` | [`busybox`](../schemas/last/busybox.json), [`freebsd`](../schemas/last/freebsd.json), [`freebsd-year`](../schemas/last/freebsd-year.json) |
| `launchctl` | [`list`](../schemas/launchctl/list.json) |
| `ldconfig` | [`cache`](../schemas/ldconfig/cache.json) |
| `ldd` | [`files`](../schemas/ldd/files.json), [`posix`](../schemas/ldd/posix.json) |
| `lipo` | [`info`](../schemas/lipo/info.json) |
| `locale` | [`keywords`](../schemas/locale/keywords.json), [`locales`](../schemas/locale/locales.json), [`posix`](../schemas/locale/posix.json) |
| `localectl` | [`linux`](../schemas/localectl/linux.json) |
| `loginctl` | [`seats`](../schemas/loginctl/seats.json), [`sessions`](../schemas/loginctl/sessions.json), [`show`](../schemas/loginctl/show.json), [`users`](../schemas/loginctl/users.json) |
| `losetup` | [`associations`](../schemas/losetup/associations.json), [`linux`](../schemas/losetup/linux.json), [`raw`](../schemas/losetup/raw.json) |
| `ls` | [`full-time`](../schemas/ls/full-time.json), [`long`](../schemas/ls/long.json), [`long-context`](../schemas/ls/long-context.json), [`long-inode`](../schemas/ls/long-inode.json), [`long-iso`](../schemas/ls/long-iso.json), [`long-no-group`](../schemas/ls/long-no-group.json), [`long-no-owner`](../schemas/ls/long-no-owner.json), [`long-no-owner-group`](../schemas/ls/long-no-owner-group.json), [`long-recursive`](../schemas/ls/long-recursive.json), [`names`](../schemas/ls/names.json), [`names-zero`](../schemas/ls/names-zero.json) |
| `lsattr` | [`linux`](../schemas/lsattr/linux.json) |
| `lsb_release` | [`linux`](../schemas/lsb_release/linux.json) |
| `lsblk` | [`bytes`](../schemas/lsblk/bytes.json), [`filesystems`](../schemas/lsblk/filesystems.json), [`filesystems-raw`](../schemas/lsblk/filesystems-raw.json), [`linux`](../schemas/lsblk/linux.json), [`no-headings`](../schemas/lsblk/no-headings.json), [`pairs`](../schemas/lsblk/pairs.json), [`permissions`](../schemas/lsblk/permissions.json), [`permissions-raw`](../schemas/lsblk/permissions-raw.json), [`raw`](../schemas/lsblk/raw.json), [`topology`](../schemas/lsblk/topology.json), [`topology-raw`](../schemas/lsblk/topology-raw.json) |
| `lsclocks` | [`linux`](../schemas/lsclocks/linux.json) |
| `lscpu` | [`caches`](../schemas/lscpu/caches.json), [`extended`](../schemas/lscpu/extended.json), [`linux`](../schemas/lscpu/linux.json) |
| `lsfd` | [`linux`](../schemas/lsfd/linux.json), [`raw`](../schemas/lsfd/raw.json) |
| `lshw` | [`businfo`](../schemas/lshw/businfo.json), [`short`](../schemas/lshw/short.json) |
| `lsipc` | [`linux`](../schemas/lsipc/linux.json), [`queues`](../schemas/lsipc/queues.json), [`raw`](../schemas/lsipc/raw.json), [`semaphores`](../schemas/lsipc/semaphores.json), [`shmems`](../schemas/lsipc/shmems.json) |
| `lsirq` | [`linux`](../schemas/lsirq/linux.json) |
| `lslocks` | [`linux`](../schemas/lslocks/linux.json), [`raw`](../schemas/lslocks/raw.json) |
| `lslogins` | [`linux`](../schemas/lslogins/linux.json), [`raw`](../schemas/lslogins/raw.json) |
| `lsmem` | [`linux`](../schemas/lsmem/linux.json) |
| `lsmod` | [`busybox`](../schemas/lsmod/busybox.json), [`linux`](../schemas/lsmod/linux.json) |
| `lsns` | [`linux`](../schemas/lsns/linux.json) |
| `lsof` | [`linux`](../schemas/lsof/linux.json), [`tasks`](../schemas/lsof/tasks.json) |
| `lspci` | [`kernel`](../schemas/lspci/kernel.json), [`linux`](../schemas/lspci/linux.json), [`machine`](../schemas/lspci/machine.json), [`numeric`](../schemas/lspci/numeric.json), [`numeric-names`](../schemas/lspci/numeric-names.json), [`verbose`](../schemas/lspci/verbose.json), [`verbose-machine`](../schemas/lspci/verbose-machine.json) |
| `lsusb` | [`linux`](../schemas/lsusb/linux.json), [`tree`](../schemas/lsusb/tree.json), [`verbose`](../schemas/lsusb/verbose.json) |
| `lz4` | [`list`](../schemas/lz4/list.json) |
| `mc` | [`ls`](../schemas/mc/ls.json) |
| `md5` | [`bsd`](../schemas/md5/bsd.json) |
| `md5sum` | [`check`](../schemas/md5sum/check.json), [`posix`](../schemas/md5sum/posix.json) |
| `memory_pressure` | [`darwin`](../schemas/memory_pressure/darwin.json) |
| `mise` | [`ls`](../schemas/mise/ls.json), [`outdated`](../schemas/mise/outdated.json) |
| `modinfo` | [`linux`](../schemas/modinfo/linux.json) |
| `mokutil` | [`sbat`](../schemas/mokutil/sbat.json) |
| `mount` | [`bsd`](../schemas/mount/bsd.json), [`linux`](../schemas/mount/linux.json) |
| `mpstat` | [`interrupts`](../schemas/mpstat/interrupts.json), [`linux`](../schemas/mpstat/linux.json) |
| `mtr` | [`report`](../schemas/mtr/report.json) |
| `namei` | [`long`](../schemas/namei/long.json) |
| `net` | [`localgroup`](../schemas/net/localgroup.json), [`share`](../schemas/net/share.json), [`start`](../schemas/net/start.json), [`user`](../schemas/net/user.json) |
| `netstat` | [`all-sockets`](../schemas/netstat/all-sockets.json), [`darwin-interface`](../schemas/netstat/darwin-interface.json), [`freebsd-interface`](../schemas/netstat/freebsd-interface.json), [`freebsd-interface-bytes`](../schemas/netstat/freebsd-interface-bytes.json), [`freebsd-routing`](../schemas/netstat/freebsd-routing.json), [`interface`](../schemas/netstat/interface.json), [`internet`](../schemas/netstat/internet.json), [`routing`](../schemas/netstat/routing.json), [`unix`](../schemas/netstat/unix.json), [`windows`](../schemas/netstat/windows.json), [`windows-ethernet`](../schemas/netstat/windows-ethernet.json), [`windows-pid`](../schemas/netstat/windows-pid.json) |
| `networkctl` | [`list`](../schemas/networkctl/list.json) |
| `networksetup` | [`hardware-ports`](../schemas/networksetup/hardware-ports.json) |
| `nm` | [`dynamic`](../schemas/nm/dynamic.json) |
| `nmcli` | [`connection`](../schemas/nmcli/connection.json), [`device`](../schemas/nmcli/device.json), [`device-show`](../schemas/nmcli/device-show.json), [`device-terse`](../schemas/nmcli/device-terse.json), [`general`](../schemas/nmcli/general.json), [`radio`](../schemas/nmcli/radio.json), [`wifi`](../schemas/nmcli/wifi.json) |
| `npm` | [`ls`](../schemas/npm/ls.json), [`ls-all`](../schemas/npm/ls-all.json), [`outdated`](../schemas/npm/outdated.json) |
| `nslookup` | [`query`](../schemas/nslookup/query.json) |
| `nstat` | [`counters`](../schemas/nstat/counters.json) |
| `numactl` | [`hardware`](../schemas/numactl/hardware.json), [`show`](../schemas/numactl/show.json) |
| `objdump` | [`section-headers`](../schemas/objdump/section-headers.json) |
| `od` | [`hex-bytes`](../schemas/od/hex-bytes.json) |
| `openssl` | [`ciphers`](../schemas/openssl/ciphers.json), [`ciphers-codes`](../schemas/openssl/ciphers-codes.json), [`version-all`](../schemas/openssl/version-all.json), [`x509-fields`](../schemas/openssl/x509-fields.json) |
| `otool` | [`libraries`](../schemas/otool/libraries.json) |
| `pactl` | [`info`](../schemas/pactl/info.json), [`short-cards`](../schemas/pactl/short-cards.json), [`short-clients`](../schemas/pactl/short-clients.json), [`short-devices`](../schemas/pactl/short-devices.json), [`sinks`](../schemas/pactl/sinks.json) |
| `parted` | [`machine`](../schemas/parted/machine.json) |
| `passwd` | [`status`](../schemas/passwd/status.json) |
| `pdffonts` | [`poppler`](../schemas/pdffonts/poppler.json) |
| `pdfimages` | [`list`](../schemas/pdfimages/list.json) |
| `pdfinfo` | [`poppler`](../schemas/pdfinfo/poppler.json) |
| `pgrep` | [`list-name`](../schemas/pgrep/list-name.json) |
| `pidstat` | [`io`](../schemas/pidstat/io.json), [`kernel`](../schemas/pidstat/kernel.json), [`linux`](../schemas/pidstat/linux.json), [`memory`](../schemas/pidstat/memory.json), [`priority`](../schemas/pidstat/priority.json), [`stack`](../schemas/pidstat/stack.json), [`switches`](../schemas/pidstat/switches.json), [`threads`](../schemas/pidstat/threads.json), [`user`](../schemas/pidstat/user.json) |
| `ping` | [`bsd`](../schemas/ping/bsd.json), [`linux`](../schemas/ping/linux.json) |
| `pip` | [`columns`](../schemas/pip/columns.json), [`columns-outdated`](../schemas/pip/columns-outdated.json), [`freeze`](../schemas/pip/freeze.json) |
| `pkg-config` | [`list`](../schemas/pkg-config/list.json) |
| `pmap` | [`extended`](../schemas/pmap/extended.json), [`linux`](../schemas/pmap/linux.json) |
| `pmset` | [`settings`](../schemas/pmset/settings.json) |
| `powerprofilesctl` | [`list`](../schemas/powerprofilesctl/list.json) |
| `prlimit` | [`linux`](../schemas/prlimit/linux.json) |
| `proc` | [`buddyinfo`](../schemas/proc/buddyinfo.json), [`cgroups`](../schemas/proc/cgroups.json), [`consoles`](../schemas/proc/consoles.json), [`cpuinfo-x86`](../schemas/proc/cpuinfo-x86.json), [`crypto`](../schemas/proc/crypto.json), [`devices`](../schemas/proc/devices.json), [`diskstats`](../schemas/proc/diskstats.json), [`filesystems`](../schemas/proc/filesystems.json), [`interrupts`](../schemas/proc/interrupts.json), [`loadavg`](../schemas/proc/loadavg.json), [`meminfo`](../schemas/proc/meminfo.json), [`modules`](../schemas/proc/modules.json), [`mountinfo`](../schemas/proc/mountinfo.json), [`net-arp`](../schemas/proc/net-arp.json), [`net-dev`](../schemas/proc/net-dev.json), [`net-if-inet6`](../schemas/proc/net-if-inet6.json), [`net-route`](../schemas/proc/net-route.json), [`net-sockstat`](../schemas/proc/net-sockstat.json), [`net-unix`](../schemas/proc/net-unix.json), [`partitions`](../schemas/proc/partitions.json), [`pressure`](../schemas/proc/pressure.json), [`schedstat`](../schemas/proc/schedstat.json), [`self-io`](../schemas/proc/self-io.json), [`self-limits`](../schemas/proc/self-limits.json), [`self-maps`](../schemas/proc/self-maps.json), [`self-status`](../schemas/proc/self-status.json), [`softirqs`](../schemas/proc/softirqs.json), [`stat`](../schemas/proc/stat.json), [`uptime`](../schemas/proc/uptime.json), [`vmstat`](../schemas/proc/vmstat.json) |
| `procs` | [`default`](../schemas/procs/default.json) |
| `ps` | [`bsd`](../schemas/ps/bsd.json), [`bsd-short`](../schemas/ps/bsd-short.json), [`busybox`](../schemas/ps/busybox.json), [`freebsd`](../schemas/ps/freebsd.json), [`freebsd-long`](../schemas/ps/freebsd-long.json), [`full-format`](../schemas/ps/full-format.json), [`jobs`](../schemas/ps/jobs.json), [`long`](../schemas/ps/long.json), [`long-y`](../schemas/ps/long-y.json), [`posix`](../schemas/ps/posix.json), [`threads`](../schemas/ps/threads.json), [`unix`](../schemas/ps/unix.json) |
| `pwdx` | [`default`](../schemas/pwdx/default.json) |
| `rclone` | [`lsd`](../schemas/rclone/lsd.json), [`lsl`](../schemas/rclone/lsl.json), [`version`](../schemas/rclone/version.json) |
| `readelf` | [`dynamic`](../schemas/readelf/dynamic.json), [`header`](../schemas/readelf/header.json), [`sections-wide`](../schemas/readelf/sections-wide.json), [`symbols-wide`](../schemas/readelf/symbols-wide.json) |
| `redis-cli` | [`client-list`](../schemas/redis-cli/client-list.json), [`info`](../schemas/redis-cli/info.json) |
| `resolvectl` | [`per-link`](../schemas/resolvectl/per-link.json), [`query`](../schemas/resolvectl/query.json), [`status`](../schemas/resolvectl/status.json) |
| `rfkill` | [`linux`](../schemas/rfkill/linux.json), [`list`](../schemas/rfkill/list.json) |
| `route` | [`linux`](../schemas/route/linux.json), [`linux-extended`](../schemas/route/linux-extended.json), [`linux-inet6`](../schemas/route/linux-inet6.json), [`windows`](../schemas/route/windows.json) |
| `rpm` | [`info`](../schemas/rpm/info.json) |
| `rsync` | [`itemize`](../schemas/rsync/itemize.json) |
| `rustc` | [`verbose-version`](../schemas/rustc/verbose-version.json) |
| `rustup` | [`check`](../schemas/rustup/check.json), [`component-list`](../schemas/rustup/component-list.json), [`target-list`](../schemas/rustup/target-list.json), [`toolchains`](../schemas/rustup/toolchains.json), [`toolchains-verbose`](../schemas/rustup/toolchains-verbose.json) |
| `sar` | [`cpu`](../schemas/sar/cpu.json), [`cpu-all`](../schemas/sar/cpu-all.json), [`cpu-frequency`](../schemas/sar/cpu-frequency.json), [`disk`](../schemas/sar/disk.json), [`filesystem`](../schemas/sar/filesystem.json), [`hugepages`](../schemas/sar/hugepages.json), [`icmp`](../schemas/sar/icmp.json), [`icmp-errors`](../schemas/sar/icmp-errors.json), [`icmp6`](../schemas/sar/icmp6.json), [`icmp6-errors`](../schemas/sar/icmp6-errors.json), [`interrupts`](../schemas/sar/interrupts.json), [`io`](../schemas/sar/io.json), [`ip`](../schemas/sar/ip.json), [`ip-errors`](../schemas/sar/ip-errors.json), [`ip6`](../schemas/sar/ip6.json), [`ip6-errors`](../schemas/sar/ip6-errors.json), [`kernel-tables`](../schemas/sar/kernel-tables.json), [`memory`](../schemas/sar/memory.json), [`memory-all`](../schemas/sar/memory-all.json), [`network-device`](../schemas/sar/network-device.json), [`network-errors`](../schemas/sar/network-errors.json), [`nfs-client`](../schemas/sar/nfs-client.json), [`nfs-server`](../schemas/sar/nfs-server.json), [`paging`](../schemas/sar/paging.json), [`queue`](../schemas/sar/queue.json), [`sockets`](../schemas/sar/sockets.json), [`sockets6`](../schemas/sar/sockets6.json), [`softnet`](../schemas/sar/softnet.json), [`swap`](../schemas/sar/swap.json), [`swapping`](../schemas/sar/swapping.json), [`task`](../schemas/sar/task.json), [`tcp`](../schemas/sar/tcp.json), [`tcp-errors`](../schemas/sar/tcp-errors.json), [`udp`](../schemas/sar/udp.json), [`udp6`](../schemas/sar/udp6.json) |
| `sc` | [`query`](../schemas/sc/query.json), [`queryex`](../schemas/sc/queryex.json) |
| `scc` | [`default`](../schemas/scc/default.json) |
| `schtasks` | [`list`](../schemas/schtasks/list.json), [`table`](../schemas/schtasks/table.json) |
| `screen` | [`list`](../schemas/screen/list.json) |
| `scutil` | [`dns`](../schemas/scutil/dns.json) |
| `sensors` | [`linux`](../schemas/sensors/linux.json), [`no-adapter`](../schemas/sensors/no-adapter.json), [`raw`](../schemas/sensors/raw.json) |
| `service` | [`status-all`](../schemas/service/status-all.json) |
| `sfdisk` | [`dump`](../schemas/sfdisk/dump.json) |
| `sha1sum` | [`posix`](../schemas/sha1sum/posix.json) |
| `sha224sum` | [`posix`](../schemas/sha224sum/posix.json) |
| `sha256sum` | [`posix`](../schemas/sha256sum/posix.json) |
| `sha384sum` | [`posix`](../schemas/sha384sum/posix.json) |
| `sha512sum` | [`posix`](../schemas/sha512sum/posix.json) |
| `shellcheck` | [`gcc`](../schemas/shellcheck/gcc.json) |
| `size` | [`gnu`](../schemas/size/gnu.json), [`sysv`](../schemas/size/sysv.json) |
| `smartctl` | [`scan`](../schemas/smartctl/scan.json) |
| `snap` | [`aliases`](../schemas/snap/aliases.json), [`changes`](../schemas/snap/changes.json), [`connections`](../schemas/snap/connections.json), [`list`](../schemas/snap/list.json), [`refresh-list`](../schemas/snap/refresh-list.json), [`services`](../schemas/snap/services.json), [`version`](../schemas/snap/version.json) |
| `sockstat` | [`freebsd`](../schemas/sockstat/freebsd.json), [`freebsd-state`](../schemas/sockstat/freebsd-state.json) |
| `spctl` | [`status`](../schemas/spctl/status.json) |
| `ss` | [`connected`](../schemas/ss/connected.json), [`linux`](../schemas/ss/linux.json), [`single-protocol`](../schemas/ss/single-protocol.json), [`summary`](../schemas/ss/summary.json) |
| `ssh-add` | [`public-keys`](../schemas/ssh-add/public-keys.json) |
| `ssh-keygen` | [`fingerprint`](../schemas/ssh-keygen/fingerprint.json) |
| `ssh-keyscan` | [`known-hosts`](../schemas/ssh-keyscan/known-hosts.json) |
| `stat` | [`bsd`](../schemas/stat/bsd.json), [`bsd-verbose`](../schemas/stat/bsd-verbose.json), [`gnu`](../schemas/stat/gnu.json), [`gnu-filesystem`](../schemas/stat/gnu-filesystem.json), [`gnu-terse`](../schemas/stat/gnu-terse.json) |
| `staticcheck` | [`default`](../schemas/staticcheck/default.json) |
| `sum` | [`bsd`](../schemas/sum/bsd.json) |
| `sw_vers` | [`darwin`](../schemas/sw_vers/darwin.json) |
| `swapinfo` | [`freebsd`](../schemas/swapinfo/freebsd.json), [`freebsd-human`](../schemas/swapinfo/freebsd-human.json) |
| `swapon` | [`legacy`](../schemas/swapon/legacy.json), [`linux`](../schemas/swapon/linux.json) |
| `sysctl` | [`freebsd`](../schemas/sysctl/freebsd.json), [`linux`](../schemas/sysctl/linux.json) |
| `syslog` | [`rfc3164`](../schemas/syslog/rfc3164.json), [`rfc5424`](../schemas/syslog/rfc5424.json) |
| `system_profiler` | [`data-type`](../schemas/system_profiler/data-type.json) |
| `systemctl` | [`automounts`](../schemas/systemctl/automounts.json), [`dependencies`](../schemas/systemctl/dependencies.json), [`jobs`](../schemas/systemctl/jobs.json), [`machines`](../schemas/systemctl/machines.json), [`paths`](../schemas/systemctl/paths.json), [`show`](../schemas/systemctl/show.json), [`show-properties`](../schemas/systemctl/show-properties.json), [`sockets`](../schemas/systemctl/sockets.json), [`status`](../schemas/systemctl/status.json), [`timers`](../schemas/systemctl/timers.json), [`unit-files`](../schemas/systemctl/unit-files.json), [`units`](../schemas/systemctl/units.json), [`units-jobs`](../schemas/systemctl/units-jobs.json), [`units-jobs-plain`](../schemas/systemctl/units-jobs-plain.json) |
| `systemd-analyze` | [`blame`](../schemas/systemd-analyze/blame.json), [`calendar`](../schemas/systemd-analyze/calendar.json), [`critical-chain`](../schemas/systemd-analyze/critical-chain.json), [`security`](../schemas/systemd-analyze/security.json), [`time`](../schemas/systemd-analyze/time.json), [`timespan`](../schemas/systemd-analyze/timespan.json), [`timestamp`](../schemas/systemd-analyze/timestamp.json) |
| `systemd-cgls` | [`tree`](../schemas/systemd-cgls/tree.json) |
| `systemd-cgtop` | [`batch`](../schemas/systemd-cgtop/batch.json) |
| `systemd-delta` | [`no-diff`](../schemas/systemd-delta/no-diff.json) |
| `systemd-id128` | [`show`](../schemas/systemd-id128/show.json) |
| `systemd-inhibit` | [`list`](../schemas/systemd-inhibit/list.json) |
| `systemd-path` | [`paths`](../schemas/systemd-path/paths.json) |
| `systeminfo` | [`windows`](../schemas/systeminfo/windows.json) |
| `table` | [`aligned`](../schemas/table/aligned.json), [`box`](../schemas/table/box.json), [`whitespace`](../schemas/table/whitespace.json) |
| `tar` | [`busybox`](../schemas/tar/busybox.json), [`gnu`](../schemas/tar/gnu.json) |
| `task` | [`list`](../schemas/task/list.json) |
| `tasklist` | [`csv`](../schemas/tasklist/csv.json), [`list`](../schemas/tasklist/list.json), [`modules`](../schemas/tasklist/modules.json), [`services`](../schemas/tasklist/services.json), [`table`](../schemas/tasklist/table.json), [`verbose`](../schemas/tasklist/verbose.json) |
| `taskset` | [`affinity-list`](../schemas/taskset/affinity-list.json), [`affinity-mask`](../schemas/taskset/affinity-mask.json) |
| `tc` | [`qdisc`](../schemas/tc/qdisc.json), [`qdisc-stats`](../schemas/tc/qdisc-stats.json) |
| `timedatectl` | [`linux`](../schemas/timedatectl/linux.json), [`show`](../schemas/timedatectl/show.json), [`timesync`](../schemas/timedatectl/timesync.json), [`timezones`](../schemas/timedatectl/timezones.json) |
| `tokei` | [`default`](../schemas/tokei/default.json) |
| `top` | [`busybox`](../schemas/top/busybox.json), [`darwin`](../schemas/top/darwin.json), [`linux`](../schemas/top/linux.json) |
| `tracepath` | [`linux`](../schemas/tracepath/linux.json) |
| `tree` | [`listing`](../schemas/tree/listing.json) |
| `trust` | [`list`](../schemas/trust/list.json) |
| `udevadm` | [`info`](../schemas/udevadm/info.json) |
| `ulimit` | [`bash`](../schemas/ulimit/bash.json), [`dash`](../schemas/ulimit/dash.json) |
| `uname` | [`darwin`](../schemas/uname/darwin.json), [`freebsd`](../schemas/uname/freebsd.json), [`linux`](../schemas/uname/linux.json) |
| `unzip` | [`busybox`](../schemas/unzip/busybox.json), [`info-zip`](../schemas/unzip/info-zip.json) |
| `update-alternatives` | [`query`](../schemas/update-alternatives/query.json), [`selections`](../schemas/update-alternatives/selections.json) |
| `upower` | [`device`](../schemas/upower/device.json), [`dump`](../schemas/upower/dump.json), [`enumerate`](../schemas/upower/enumerate.json) |
| `uptime` | [`bsd`](../schemas/uptime/bsd.json), [`freebsd`](../schemas/uptime/freebsd.json), [`linux`](../schemas/uptime/linux.json), [`pretty`](../schemas/uptime/pretty.json), [`since`](../schemas/uptime/since.json) |
| `uuidparse` | [`linux`](../schemas/uuidparse/linux.json), [`raw`](../schemas/uuidparse/raw.json) |
| `uv` | [`pip-show`](../schemas/uv/pip-show.json), [`python-list`](../schemas/uv/python-list.json), [`tool-list`](../schemas/uv/tool-list.json), [`tree`](../schemas/uv/tree.json) |
| `ver` | [`windows`](../schemas/ver/windows.json) |
| `vm_stat` | [`darwin`](../schemas/vm_stat/darwin.json) |
| `vmstat` | [`freebsd-interrupts`](../schemas/vmstat/freebsd-interrupts.json), [`linux`](../schemas/vmstat/linux.json), [`linux-active`](../schemas/vmstat/linux-active.json), [`linux-disk`](../schemas/vmstat/linux-disk.json), [`linux-disk-summary`](../schemas/vmstat/linux-disk-summary.json), [`linux-partition`](../schemas/vmstat/linux-partition.json), [`linux-stats`](../schemas/vmstat/linux-stats.json) |
| `w` | [`bsd`](../schemas/w/bsd.json), [`freebsd`](../schemas/w/freebsd.json), [`linux`](../schemas/w/linux.json), [`linux-short`](../schemas/w/linux-short.json) |
| `wc` | [`posix`](../schemas/wc/posix.json) |
| `who` | [`iso`](../schemas/who/iso.json), [`posix`](../schemas/who/posix.json) |
| `whoami` | [`groups`](../schemas/whoami/groups.json), [`privileges`](../schemas/whoami/privileges.json) |
| `xinput` | [`list`](../schemas/xinput/list.json) |
| `xrandr` | [`linux`](../schemas/xrandr/linux.json), [`listmonitors`](../schemas/xrandr/listmonitors.json) |
| `xxd` | [`default`](../schemas/xxd/default.json) |
| `xz` | [`list`](../schemas/xz/list.json), [`list-robot`](../schemas/xz/list-robot.json) |
| `zfs` | [`list`](../schemas/zfs/list.json) |
| `zipinfo` | [`default`](../schemas/zipinfo/default.json) |
| `zpool` | [`list`](../schemas/zpool/list.json) |
| `zstd` | [`list`](../schemas/zstd/list.json), [`list-verbose`](../schemas/zstd/list-verbose.json) |

A variant is one output format of a command. GNU `df`, `df -h`, macOS
`df` and BusyBox `df -h` are four formats, so they are four definitions.
When several implementations print the same format, one definition covers
them and says so in its metadata.

Some commands print what another command prints. Nothing in the text says
which of them wrote it, so they share a definition rather than having one
each, and the command they are listed under is the one the definition is
named for. These are read as well:

`7za` (as `7z`), `arecord` (as `aplay`), `b2sum` (as `md5`, `md5sum`, `sha256sum`, `sha384sum`, `sha512sum`), `batcat` (as `bat`), `cksum` (as `md5`, `md5sum`, `sha1sum`, `sha224sum`, `sha256sum`, `sha384sum`, `sha512sum`, `sum`), `dpkg-deb` (as `tar`), `gb2sum` (as `md5`, `md5sum`, `sha256sum`, `sha384sum`, `sha512sum`), `gcksum` (as `cksum`, `md5sum`, `sha1sum`, `sha224sum`, `sha256sum`, `sha384sum`, `sha512sum`), `gdate` (as `date`), `gdf` (as `df`), `gdu` (as `du`), `genv` (as `env`), `getent` (as `etc`), `gfactor` (as `factor`), `gid` (as `id`), `gls` (as `ls`), `gmd5sum` (as `md5`, `md5sum`), `gpg2` (as `gpg`), `gsha1sum` (as `md5`, `sha1sum`), `gsha224sum` (as `md5`, `sha224sum`), `gsha256sum` (as `md5`, `sha256sum`), `gsha384sum` (as `md5`, `sha384sum`), `gsha512sum` (as `md5`, `sha512sum`), `gstat` (as `stat`), `gsum` (as `sum`), `gtar` (as `tar`), `guname` (as `uname`), `gwc` (as `wc`), `gwho` (as `who`), `hd` (as `hexdump`), `mcli` (as `mc`), `md5sum` (as `md5`), `nerdctl` (as `docker`), `netstat` (as `route`), `ping6` (as `ping`), `pip3` (as `pip`), `podman` (as `docker`), `printenv` (as `env`), `python` (as `pip`), `python3` (as `pip`), `rmd160` (as `md5`), `route` (as `netstat`), `sha1` (as `md5`), `sha1sum` (as `md5`, `md5sum`), `sha224` (as `md5`), `sha224sum` (as `md5`, `md5sum`), `sha256` (as `md5`), `sha256sum` (as `md5`, `md5sum`), `sha384` (as `md5`), `sha384sum` (as `md5`, `md5sum`), `sha512` (as `md5`), `sha512sum` (as `md5`, `md5sum`), `sha512t224` (as `md5`), `sha512t256` (as `md5`), `shasum` (as `md5`, `sha1sum`, `sha224sum`, `sha256sum`, `sha384sum`, `sha512sum`), `skein1024` (as `md5`), `skein256` (as `md5`), `skein512` (as `md5`), `ssh-add` (as `ssh-keygen`), `systemd-resolve` (as `resolvectl`), `uv` (as `pip`), `vdir` (as `ls`)

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
