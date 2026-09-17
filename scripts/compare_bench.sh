#!/usr/bin/env bash
# Time jz against jc and jo on the same input, on this machine.
#
# jc (https://github.com/kellyjonbrazil/jc, MIT) and jo
# (https://github.com/jpmens/jo, GPL-2.0-or-later) do parts of what jz does. This
# script is not a test and nothing in the repository depends on it; it is
# run by hand to produce the figures on the comparison page, and it needs
# hyperfine, GNU time (/usr/bin/time), python3, jq, taskset (optional),
# and the three tools:
#
#   JZ=./jz JC=/path/to/jc JO=/path/to/jo scripts/compare_bench.sh [OUTDIR]
#
# Command output is timed three ways where jz has them: detected from
# the text, with the parser named, and with jz running the command, since
# a named parser reads only its own definitions and detection reads all.
#
# The inputs are made on the spot: `df -h` and `ps aux` as this machine
# prints them, `ps aux` repeated to 100 000 rows, and a generated CSV of
# 100 000 rows. Before timing, the record counts of both tools are checked
# to be equal, so a tool that stopped early is not timed as a fast one.
#
# The figures say how long one process takes and how much memory it
# holds at most. They say nothing about what the JSON contains, which is
# on the coverage page.
set -euo pipefail

JZ=${JZ:-jz}
JC=${JC:-jc}
JO=${JO:-jo}
OUT=${1:-bench-compare}
RUNS=${RUNS:-30}

mkdir -p "$OUT"
cd "$OUT"

LC_ALL=C df -h > df.txt
LC_ALL=C ps aux > ps.txt
{
	head -n 1 ps.txt
	tail -n +2 ps.txt | awk -v n=100000 '{ a[NR] = $0 } END { for (i = 0; i < n; i++) print a[i % NR + 1] }'
} > ps-100k.txt
python3 - <<'EOF'
import csv, random
random.seed(1)
with open("data-100k.csv", "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["id", "name", "city", "score"])
    for i in range(100000):
        w.writerow([i, "user%d" % i, random.choice(["Tokyo", "Osaka", "New York, NY"]), random.random()])
EOF

same() {
	local what=$1 a b
	a=$(eval "$2" | jq -s 'if length == 1 and (.[0] | type) == "array" then .[0] | length else length end')
	b=$(eval "$3" | jq -s 'if length == 1 and (.[0] | type) == "array" then .[0] | length else length end')
	if [ "$a" != "$b" ]; then
		echo "$what: jz produced $a records and the other $b; not timing it" >&2
		exit 1
	fi
	echo "$what: $a records from each"
}

same df "$JZ < df.txt" "$JC --df < df.txt"
same run-df "$JZ run df -h" "$JC df -h"
same ps-100k "$JZ < ps-100k.txt" "$JC --ps < ps-100k.txt"
same csv-100k "$JZ --format csv < data-100k.csv" "$JC --csv < data-100k.csv"
same csv-100k-stream "$JZ --format csv --stream < data-100k.csv" "$JC --csv-s < data-100k.csv"

# Each case runs twice: on every core the machine has, and pinned to one
# core with taskset. jz loads its definitions on several threads, so its
# time on all cores is shorter than its CPU time, and a machine with fewer
# cores sees a figure closer to the pinned one. The table prints both,
# with the user and system CPU time hyperfine measured. Starting taskset
# adds a fraction of a millisecond to every pinned run, which is most of
# the pinned figure of the jo and jz new cases.
if command -v taskset > /dev/null; then
	PIN="taskset -c 0 "
else
	PIN=""
	echo "taskset not found: the one-core rows are skipped" >&2
fi

echo "| Input | Command | Cores | Mean [ms] | User [ms] | System [ms] |"
echo "|---|---|---|---|---|---|"

hf() {
	local input=$1 label=$2 cmd=$3 cores prefix args
	for cores in all one; do
		prefix=""
		if [ "$cores" = one ]; then
			[ -n "$PIN" ] || continue
			prefix=$PIN
		fi
		args=(--shell=none --warmup 3 --runs "$RUNS" --export-json run.json)
		if [ "$input" != - ]; then
			args+=(--input "$input")
		fi
		hyperfine "${args[@]}" "$prefix$cmd" > /dev/null
		jq -r --arg input "$input" --arg label "$label" --arg cores "$cores" \
			'.results[0] | "| \($input) | \($label) | \($cores) | \(.mean * 1000 * 10 | round / 10) ± \(.stddev * 1000 * 10 | round / 10) | \(.user * 1000 * 10 | round / 10) | \(.system * 1000 * 10 | round / 10) |"' run.json
	done
}

hf df.txt "jz (detected)" "$JZ"
hf df.txt "jz --parser df" "$JZ --parser df"
hf df.txt "jc --df" "$JC --df"
hf - "jz run df -h" "$JZ run df -h"
hf - "jc df -h" "$JC df -h"
hf ps-100k.txt "jz (detected)" "$JZ"
hf ps-100k.txt "jz --parser ps" "$JZ --parser ps"
hf ps-100k.txt "jc --ps" "$JC --ps"
hf data-100k.csv "jz --format csv" "$JZ --format csv"
hf data-100k.csv "jc --csv" "$JC --csv"
hf data-100k.csv "jz --format csv --stream" "$JZ --format csv --stream"
hf data-100k.csv "jc --csv-s" "$JC --csv-s"
hf - "jz new" "$JZ new name=api replicas:=3 debug:=false tags[]=web tags[]=prod"
hf - "jo" "$JO name=api replicas=3 debug=false tags[]=web tags[]=prod"
hf - "jz new --path" "$JZ new --path /metadata/labels/app=api --path /spec/replicas:=3"
hf - "jo -d." "$JO -d. metadata.labels.app=api spec.replicas=3"
echo

# Peak memory is the median of five runs of GNU time's maximum resident
# set size, on all cores and pinned to one, since a Go program started on
# fewer cores can hold a different amount.
echo "| Input | Command | Cores | Max RSS [MiB] |"
echo "|---|---|---|---|"

rss() {
	local input=$1 label=$2 cmd=$3 cores prefix
	for cores in all one; do
		prefix=""
		if [ "$cores" = one ]; then
			[ -n "$PIN" ] || continue
			prefix=$PIN
		fi
		: > rss.txt
		for _ in 1 2 3 4 5; do
			# shellcheck disable=SC2086 # the command is split into words on purpose
			/usr/bin/time -o rss.txt -a -f "%M" $prefix$cmd < "$input" > /dev/null
		done
		sort -n rss.txt | sed -n 3p | awk -v i="$input" -v l="$label" -v c="$cores" '{ printf "| %s | %s | %s | %.1f |\n", i, l, c, $1 / 1024 }'
	done
}

rss df.txt "jz (detected)" "$JZ"
rss df.txt "jz --parser df" "$JZ --parser df"
rss df.txt "jc --df" "$JC --df"
rss ps-100k.txt "jz (detected)" "$JZ"
rss ps-100k.txt "jc --ps" "$JC --ps"
rss data-100k.csv "jz --format csv" "$JZ --format csv"
rss data-100k.csv "jc --csv" "$JC --csv"
rss data-100k.csv "jz --format csv --stream" "$JZ --format csv --stream"
rss data-100k.csv "jc --csv-s" "$JC --csv-s"
rss /dev/null "jz new" "$JZ new name=api replicas:=3 debug:=false tags[]=web tags[]=prod"
rss /dev/null "jo" "$JO name=api replicas=3 debug=false tags[]=web tags[]=prod"
