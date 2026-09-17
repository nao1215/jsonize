#!/bin/sh
# gen.sh KIND N FILE writes N records of synthetic but format-faithful input
# for the himorime suites in this directory. Both revisions of a comparison
# run the working tree's copy (${head_root}/gen.sh), so they read the same
# bytes. Only sh and awk are needed.
#
#   df ps mount env    command output as those commands print it
#   csv tsv ltsv json jsonl yaml lines nul
#                      data formats, one record per row
#   ids                JSON Lines of {"id": N}, for jz new --each
#   registry           a registry directory of N table definitions, in the
#                      layout JSONIZE_REGISTRY_PATH reads; FILE is the directory
#   registry-input     the text the middle definition of such a registry reads
set -eu

kind=$1
n=$2
out=$3

case $kind in
registry)
	rm -rf "$out"
	mkdir -p "$out"
	printf 'format: 1\nname: bench\n' > "$out/registry.yaml"
	awk -v n="$n" -v dir="$out" 'BEGIN {
		for (i = 0; i < n; i++) {
			cmd = "cmd" int(i / 4); variant = "v" (i % 4)
			path = dir "/parsers/" cmd "/" variant
			system("mkdir -p " path)
			file = path "/parser.yaml"
			printf "format: 1\ncommand: %s\nvariant: %s\n", cmd, variant > file
			printf "detect:\n  os: [linux, darwin, windows]\n  signature: {all: [\"^HEADER-%d\\\\s+COLUMN\\\\s+COLUMN$\"]}\n", i > file
			printf "parse:\n  type: table\n  header: {columns: [a, b, c]}\nfields: {b: {type: int}}\n" > file
			close(file)
		}
	}'
	exit 0
	;;
esac

awk -v kind="$kind" -v n="$n" 'BEGIN {
	if (kind == "df") {
		print "Filesystem     1K-blocks    Used Available Use% Mounted on"
		for (i = 0; i < n; i++)
			printf "%-14s %9d %7d %9d %3d%% /mnt/volume-%d\n", sprintf("/dev/sd%c%d", 97 + i % 26, i % 1000), 20000000 + i, 5000000 + i, 15000000 - i, i % 100, i
	} else if (kind == "ps") {
		print "USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND"
		for (i = 1; i <= n; i++)
			printf "root     %7d  0.0  0.0  27948 13124 ?        Ss   Aug24   4:50 /usr/lib/systemd/systemd --switched-root --system\n", i
	} else if (kind == "mount") {
		for (i = 0; i < n; i++)
			printf "/dev/sd%c%d on /mnt/volume-%d type ext4 (rw,relatime,errors=remount-ro,data=ordered)\n", 97 + i % 26, i, i
	} else if (kind == "env") {
		for (i = 0; i < n; i++)
			printf "VAR_%d=value number %d with some text\n", i, i
	} else if (kind == "csv" || kind == "tsv") {
		sep = kind == "csv" ? "," : "\t"
		printf "id%sname%sage%semail%scity\n", sep, sep, sep, sep
		for (i = 1; i <= n; i++)
			printf "%d%suser%d%s%d%suser%d@example.com%sTokyo\n", i, sep, i, sep, 20 + i % 50, sep, i, sep
	} else if (kind == "ltsv") {
		for (i = 1; i <= n; i++)
			printf "id:%d\tname:user%d\tage:%d\tcity:Tokyo\n", i, i, 20 + i % 50
	} else if (kind == "json") {
		printf "["
		for (i = 1; i <= n; i++)
			printf "%s{\"id\":%d,\"name\":\"user%d\",\"tags\":[\"a\",\"b\"],\"ok\":true}", (i > 1 ? "," : ""), i, i
		print "]"
	} else if (kind == "jsonl") {
		for (i = 1; i <= n; i++)
			printf "{\"id\":%d,\"host\":\"web%d\",\"tags\":[\"a\",\"b\"],\"ok\":true}\n", i, i % 16
	} else if (kind == "yaml") {
		for (i = 1; i <= n; i++)
			printf "- id: %d\n  name: user%d\n  tags: [a, b]\n", i, i
	} else if (kind == "lines") {
		for (i = 1; i <= n; i++)
			printf "/var/log/app/service-%d.log\n", i
	} else if (kind == "nul") {
		for (i = 1; i <= n; i++)
			printf "/var/log/app/service-%d.log%c", i, 0
	} else if (kind == "ids") {
		for (i = 1; i <= n; i++)
			printf "{\"id\":%d}\n", i
	} else if (kind == "registry-input") {
		mid = int(n / 2)
		printf "HEADER-%d COLUMN COLUMN\nrow %d 10K\nrow %d 20K\n", mid, mid, mid
	} else {
		print "gen.sh: unknown kind " kind > "/dev/stderr"
		exit 2
	}
}' > "$out"
