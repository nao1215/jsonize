#!/usr/bin/env python3
"""Read the same saved output with jz and with jc, and report what each one did.

jc (https://github.com/kellyjonbrazil/jc, MIT) converts many of the same
commands, so it is a second opinion on what a text holds. This script is
not a test and is not required to build or check jsonize: it needs jc
installed and is run by hand when the question is which of the two reads
a given text, and whether either of them is losing something.

    python3 scripts/compare_jc.py                 # every mapped fixture
    python3 scripts/compare_jc.py df free dig     # only these commands
    python3 scripts/compare_jc.py --verbose ss    # name every token neither read
    python3 scripts/compare_jc.py --json > out.json

What it reports per fixture:

  status    whether each tool produced JSON, refused the text, or failed
  records   how many records each produced, where the result is a list
  coverage  the share of the words of the input that reach a leaf value
            of the JSON, counted separately for each tool
  missed    the words of the input one tool carried and the other did not

The coverage figure is the part worth reading, and it is deliberately not
a comparison of shapes. The two tools name their keys differently and nest
them differently, and normalising that away would also hide a value one of
them dropped, which is the thing worth finding. So the check is made
against the input instead: a word that appears in the text and in neither
JSON is a value nobody read, and a word one tool carried and the other did
not is a difference in what was read rather than in how it was named.

A word is counted when it is not punctuation and not a column heading of
the format, which is why the figure is below 100 even for a reading that
lost nothing: headings, drawing characters and units live in the text and
belong in no value. Read the missed list, not the percentage.
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
REGISTRY = os.path.join(ROOT, "registry", "parsers")

# Which jc parser reads the same text as a jsonize definition. A jsonize
# variant names an output format and a jc parser names a command, so the
# mapping is many to one; a variant with no entry here is one jc has no
# parser for, and is left out rather than compared against something else.
MAPPING = {
    "df/gnu": "df",
    "df/gnu-human": "df",
    "df/gnu-type": "df",
    "df/gnu-type-human": "df",
    "df/gnu-inodes": "df",
    "df/gnu-inodes-human": "df",
    "df/gnu-blocks": "df",
    "df/gnu-blocks-type": "df",
    "df/portable": "df",
    "df/portable-type": "df",
    "df/busybox-human": "df",
    "df/bsd": "df",
    "df/bsd-human": "df",
    "free/gnu": "free",
    "free/gnu-human": "free",
    "free/gnu-wide": "free",
    "free/gnu-wide-human": "free",
    "ps/unix": "ps",
    "ps/bsd": "ps",
    "ps/bsd-short": "ps",
    "ps/long": "ps",
    "ps/threads": "ps",
    "ps/posix": "ps",
    "ps/jobs": "ps",
    "ps/busybox": "ps",
    "ps/full-format": "ps",
    "ps/long-y": "ps",
    "ls/long": "ls",
    "ls/long-iso": "ls",
    "ls/full-time": "ls",
    "ls/long-inode": "ls",
    "ls/long-context": "ls",
    "ls/long-recursive": "ls",
    "ls/long-no-owner-group": "ls",
    "ls/long-no-group": "ls",
    "ls/long-no-owner": "ls",
    "ls/names": "ls",
    "lsblk/linux": "lsblk",
    "lsblk/bytes": "lsblk",
    "lsblk/filesystems": "lsblk",
    "lsblk/topology": "lsblk",
    "lsblk/permissions": "lsblk",
    "lsblk/raw": "lsblk",
    "lsblk/no-headings": "lsblk",
    "mount/linux": "mount",
    "mount/bsd": "mount",
    "ip/address": "ip_address",
    "ss/linux": "ss",
    "ss/single-protocol": "ss",
    "ss/connected": "ss",
    "netstat/internet": "netstat",
    "netstat/unix": "netstat",
    "netstat/routing": "netstat",
    "netstat/all-sockets": "netstat",
    "dig/bind": "dig",
    "dig/answers": "dig",
    "dig/axfr": "dig",
    "ping/linux": "ping",
    "ping/bsd": "ping",
    "stat/gnu": "stat",
    "stat/bsd": "stat",
    "stat/gnu-terse": "stat",
    "systemctl/units": "systemctl",
    "systemctl/unit-files": "systemctl_luf",
    "systemctl/jobs": "systemctl_lj",
    "systemctl/sockets": "systemctl_ls",
    "vmstat/linux": "vmstat",
    "vmstat/linux-wide": "vmstat",
    "iostat/linux": "iostat",
    "iostat/extended": "iostat",
    "iostat/cpu": "iostat",
    "iostat/device": "iostat",
    "iostat/linux-timestamped": "iostat",
    "ifconfig/busybox": "ifconfig",
    "findmnt/linux": "findmnt",
    "lsof/linux": "lsof",
    "lsmod/linux": "lsmod",
    "lspci/linux": "lspci",
    "lsusb/linux": "lsusb",
    "route/linux": "route",
    "arp/alternate": "arp",
    "blkid/export": "blkid",
    "blkid/linux": "blkid",
    "uname/linux": "uname",
    "uname/darwin": "uname",
    "uptime/linux": "uptime",
    "uptime/bsd": "uptime",
    "w/linux": "w",
    "w/bsd": "w",
    "who/posix": "who",
    "who/iso": "who",
    "id/posix": "id",
    "date/posix": "date",
    "du/posix": "du",
    "du/gnu-human": "du",
    "wc/posix": "wc",
    "env/posix": "env",
    "etc/passwd": "passwd",
    "etc/group": "group",
    "etc/hosts": "hosts",
    "etc/fstab": "fstab",
    "etc/os-release": "os_release",
    "etc/resolv-conf": "resolve_conf",
    "etc/crontab": "crontab",
    "proc/cpuinfo-x86": "proc_cpuinfo",
    "proc/meminfo": "proc_meminfo",
    "proc/net-dev": "proc_net_dev",
    "proc/net-route": "proc_net_route",
    "proc/partitions": "proc_partitions",
    "proc/stat": "proc_stat",
    "proc/uptime": "proc_uptime",
    "proc/modules": "proc_modules",
    "proc/mountinfo": "proc_pid_mountinfo",
    "sysctl/linux": "sysctl",
    "timedatectl/linux": "timedatectl",
    "tracepath/linux": "tracepath",
    "top/linux": "top",
    "swapon/linux": "swapon",
    "chage/linux": "chage",
    "efibootmgr/linux": "efibootmgr",
    "lsb_release/linux": "lsb_release",
    "hciconfig/linux": "hciconfig",
    "pidstat/linux": "pidstat",
    "mpstat/linux": "mpstat",
    "ethtool/settings": "ethtool",
    "nmcli/device-show": "nmcli",
    "zipinfo/default": "zipinfo",
    "xrandr/linux": "xrandr",
    "ini/default": "ini",
    "kv/equals": "kv",
    "csv/comma": "csv",
    "syslog/rfc5424": "syslog",
    "syslog/rfc3164": "syslog_bsd",
    "ipconfig/windows": "ipconfig",
    "ipconfig/all": "ipconfig",
    "systeminfo/windows": "systeminfo",
    "netstat/windows": "netstat",
    "upower/dump": "upower",
    "sfdisk/dump": "sfdisk",
    "rsync/itemize": "rsync",
    "lsattr/linux": "lsattr",
    "debconf-show/linux": "debconf_show",
    "udevadm/info": "udevadm",
    "tar/gnu": None,
}

# Words that are part of every reading of a format rather than of any
# value in it: headings, units, drawing characters and separators. A
# reading that leaves these out has lost nothing.
STRUCTURAL = re.compile(
    r"^(?:[-=+|/\\.,:;()\[\]{}<>*#~_'\"]+|total|Filesystem|Size|Used|Avail|Available|Use|Mounted|on|Type|"
    r"Inodes|IUsed|IFree|IUse|Capacity|blocks|NAME|MAJ|MIN|RM|RO|TYPE|MOUNTPOINT|MOUNTPOINTS|FSTYPE|"
    r"FSAVAIL|FSUSE|LABEL|UUID|PARTUUID|OWNER|GROUP|MODE|PID|PPID|USER|UID|GID|TTY|TIME|CMD|COMMAND|"
    r"STAT|START|STARTED|VSZ|RSS|CPU|MEM|STIME|PRI|NI|WCHAN|SZ|PSR|State|Recv|Send|Local|Address|Port|"
    r"Peer|Netid|Proto|Iface|Flags|Metric|Ref|Window|Destination|Gateway|Genmask|MSS|irtt|total|used|"
    r"free|shared|buff|cache|buffers|available|Mem|Swap|Total|avg|nice|system|iowait|steal|idle|"
    r"Device|tps|kB_read|kB_wrtn|kB_dscd|MB_read|MB_wrtn|MB_dscd|Linux|procs|swpd|si|so|bi|bo|in|cs|us|"
    r"sy|id|wa|st|memory|swap|io|cpu|b|r|Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)$",
    re.IGNORECASE,
)

WORD = re.compile(r"[^\s]+")


def words_of(text):
    """Return the words of a text that could be a value."""
    out = set()
    for w in WORD.findall(text):
        w = w.strip(",;")
        if not w or STRUCTURAL.match(w):
            continue
        out.add(w)
    return out


def leaves(value, out):
    """Collect every scalar of a JSON document as a string."""
    if isinstance(value, dict):
        for v in value.values():
            leaves(v, out)
    elif isinstance(value, list):
        for v in value:
            leaves(v, out)
    elif value is None:
        return
    elif isinstance(value, bool):
        out.add("true" if value else "false")
    else:
        out.add(str(value))
    return out


def carried(doc, want):
    """Return the words of want that appear in a leaf of doc."""
    values = leaves(doc, set())
    joined = " ".join(values)
    found = set()
    for w in want:
        if w in values or w in joined:
            found.add(w)
    return found


def run(cmd, data):
    try:
        p = subprocess.run(cmd, input=data, capture_output=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired) as exc:
        return None, str(exc)
    if p.returncode != 0:
        first = p.stderr.decode("utf-8", "replace").strip().splitlines()
        return None, (first[0] if first else "exit %d" % p.returncode)
    try:
        return json.loads(p.stdout.decode("utf-8", "replace")), None
    except json.JSONDecodeError as exc:
        return None, "output is not JSON: %s" % exc


def fixtures(commands):
    """Yield (command, variant, path) for every mapped fixture."""
    for command in sorted(os.listdir(REGISTRY)):
        if commands and command not in commands:
            continue
        cdir = os.path.join(REGISTRY, command)
        if not os.path.isdir(cdir):
            continue
        for variant in sorted(os.listdir(cdir)):
            key = "%s/%s" % (command, variant)
            if MAPPING.get(key) is None:
                continue
            td = os.path.join(cdir, variant, "testdata")
            if not os.path.isdir(td):
                continue
            for name in sorted(os.listdir(td)):
                if not name.endswith(".txt"):
                    continue
                meta = os.path.join(td, name[:-4] + ".yaml")
                if os.path.exists(meta) and "expect_error:" in open(meta).read():
                    continue
                yield command, variant, os.path.join(td, name)


def count(doc):
    if isinstance(doc, list):
        return len(doc)
    return 1


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("commands", nargs="*", help="limit to these commands")
    ap.add_argument("--jz", default=os.path.join(ROOT, "dist", "jz"), help="path to the jz binary")
    ap.add_argument("--jc", default="jc", help="path to the jc command")
    ap.add_argument("--verbose", action="store_true", help="name the words neither tool carried")
    ap.add_argument("--json", action="store_true", help="write the report as JSON")
    args = ap.parse_args()

    if not os.path.exists(args.jz):
        sys.exit("no jz binary at %s; run `make build` or pass --jz" % args.jz)
    if shutil.which(args.jc) is None and not os.path.exists(args.jc):
        sys.exit(
            "jc was not found. This comparison needs it; install it with\n"
            "  python3 -m pip install jc\n"
            "and note that no other check in this repository depends on it."
        )

    rows = []
    for command, variant, path in fixtures(args.commands):
        text = open(path, "rb").read()
        jc_parser = MAPPING["%s/%s" % (command, variant)]
        jz_doc, jz_err = run([args.jz, "--parser", command, "--variant", variant], text)
        jc_doc, jc_err = run([args.jc, "--" + jc_parser], text)
        want = words_of(text.decode("utf-8", "replace"))
        jz_words = carried(jz_doc, want) if jz_doc is not None else set()
        jc_words = carried(jc_doc, want) if jc_doc is not None else set()
        rows.append(
            {
                "definition": "%s/%s" % (command, variant),
                "jc_parser": jc_parser,
                "fixture": os.path.relpath(path, ROOT),
                "words": len(want),
                "jz": {
                    "ok": jz_doc is not None,
                    "error": jz_err,
                    "records": count(jz_doc) if jz_doc is not None else 0,
                    "carried": len(jz_words),
                },
                "jc": {
                    "ok": jc_doc is not None,
                    "error": jc_err,
                    "records": count(jc_doc) if jc_doc is not None else 0,
                    "carried": len(jc_words),
                },
                "only_jz": sorted(jz_words - jc_words),
                "only_jc": sorted(jc_words - jz_words),
                "neither": sorted(want - jz_words - jc_words),
            }
        )

    if args.json:
        json.dump(rows, sys.stdout, indent=2)
        sys.stdout.write("\n")
        return 0

    width = max([len(r["definition"]) for r in rows] + [10])
    print("%-*s  %-14s %-14s %s" % (width, "definition", "jz", "jc", "fixture"))
    for r in rows:
        def side(s):
            if not s["ok"]:
                return "refused"
            return "%d rec %d%%" % (s["records"], round(100 * s["carried"] / max(r["words"], 1)))

        print("%-*s  %-14s %-14s %s" % (width, r["definition"], side(r["jz"]), side(r["jc"]), os.path.basename(r["fixture"])))
        if not r["jz"]["ok"]:
            print("    jz: %s" % r["jz"]["error"])
        if not r["jc"]["ok"]:
            print("    jc: %s" % r["jc"]["error"])
        if r["only_jc"]:
            print("    only jc carried: %s" % " ".join(r["only_jc"][:12]))
        if r["only_jz"]:
            print("    only jz carried: %s" % " ".join(r["only_jz"][:12]))
        if args.verbose and r["neither"]:
            print("    neither carried: %s" % " ".join(r["neither"][:20]))

    refused_jz = sum(1 for r in rows if not r["jz"]["ok"])
    refused_jc = sum(1 for r in rows if not r["jc"]["ok"])
    print()
    print("%d fixtures compared; jz refused %d, jc refused %d" % (len(rows), refused_jz, refused_jc))
    return 0


if __name__ == "__main__":
    sys.exit(main())
