---
title: Compared with jc and jo
description: "What jc, jo and jsonize each do, where they differ, and how long each takes on the same input, measured on one machine."
toc: true
---

Two established tools cover most of what jz does. [jc](https://github.com/kellyjonbrazil/jc) converts the output of commands, file formats and strings to JSON. [jo](https://github.com/jpmens/jo) builds JSON from shell arguments. jz does a part of each: `COMMAND | jz` overlaps with jc, and `jz new` overlaps with jo. This page lists the differences as they stand and does not rank the tools. Each one is the better choice for some jobs.

The versions compared are jc 1.25.7 (released 2026-06-18, MIT licence), jo 1.9 (released 2022-11-04, GPL-2.0-or-later) and jz v0.8.0 (released 2026-09-20, MIT licence). jc has been developed since 2019 and jo since 2016. jz's first commit was in September 2026. jc and jo are packaged by the major Linux distributions and Homebrew. jz is installed with `go install`, a Homebrew tap, the AUR, or the packages on its release page. No code or fixture from either tool is copied into this repository.

## In short

- Use jc when you need a Python library, the Ansible filter, YAML output, line slicing, or input jz does not read: XML, TOML, plist and X.509 files; strings such as URLs, JWTs, timestamps and IP addresses; commands jz has no definition for, such as `traceroute`, `iptables`, `dmidecode`, `wg`, `ufw`, `acpi`, `mdadm`, `ntpq`, `pacman`, `find`, `finger` and `zpool status`; and more `/proc` files. When jz has to detect the format of a short output, jc was faster on one core and held less memory in the benchmarks below.
- Use jo when you need the smallest and fastest program for building JSON, type guessing (`n=1` becomes a number and `a=$(jo b=1)` nests an object without extra syntax), base64 file contents, or adding keys to an existing JSON document.
- Use jz when you want the format detected from the text instead of named, input that does not fit refused rather than read partly, parsers defined in YAML without code, or command output, data files and argument-built JSON from one binary.

## What jsonize does not do

- It does not query, sort, aggregate, or reshape JSON. Use `jq` for that.
- It does not replace a command's native JSON output. Prefer native output when available.
- It does not parse arbitrary free-form text. Input must match a known or explicitly defined format.
- It does not guess when input is ambiguous. Specify `--parser`, or conversion fails.
- It does not silently discard input that it cannot parse.
- It does not fetch parser definitions or access the network during conversion.
- It does not invoke a shell for `jz run`; shell syntax is interpreted only when you explicitly run a shell.

A definition can still name lines that are not data, such as headings, `total` lines, comments and the `df:` messages printed before a table. Those lines are left out of the JSON without a message. `--explain` counts them.

## Command output: jc and jz

| | jc 1.25.7 | jz v0.8.0 |
|---|---|---|
| Choosing the parser | named on every call (`jc --df`), or taken from the command name (`jc df -h`) | detected from the text; `--parser` narrows it, and some generic formats are read only when named |
| What is read | 236 parsers, 19 of them streaming versions of others: commands, `/proc` files, file formats and strings | 746 definitions under 271 names: commands, plus `/etc` and `/proc` files and the generic `csv`, `ini`, `kv` and `table` layouts; no string parsers |
| Commands only one of the two reads | `traceroute`, `iptables`, `dmidecode`, `wg`, `ufw`, `acpi`, `mdadm`, `ntpq`, `pacman`, `find`, `finger`, `zpool status`, ... | `systemd-*` tools, `lsfd`, `lsns`, `journalctl`, `sar`, `docker`, `sensors`, `smartctl`, ... |
| The two counts | one jc parser can cover several layouts of a command | one jz definition reads one layout, so the two counts are not comparable |
| File formats | CSV, TSV, INI, YAML, TOML, XML, plist, X.509, and more | CSV, TSV, LTSV, JSON, JSON Lines and YAML, also gzip or bzip2 compressed; INI with `--parser ini`; plain text as one string, lines, or NUL-separated records |
| Input that does not fit the parser | depends on the parser: many return an empty or partial result with exit status 0 (`echo hello world \| jc --df` prints `[]`), others stop with exit status 100 (`--uptime`, `--date`) | nothing on standard output, exit status 3 or 4, and the reason on standard error |
| Rounded sizes (`df -h` prints `13G`) | converted to a byte count | kept as the string printed |
| Values without conversion | `-r` / `--raw` | `--raw` |
| Running the command | `jc COMMAND`, without a shell; the exit status is the command's, plus 100 when jc fails | `jz run COMMAND`, without a shell, under `LC_ALL=C`; a non-zero status of the command is passed on, otherwise jz's own |
| Streaming | 19 streaming parsers (`--ls-s`, `--ping-s`, ...), JSON Lines output; `-qq` records parse errors in the output and continues | `--stream` for most formats, JSON Lines output; the first record that cannot be read ends the stream with exit status 3 |
| Output formats | JSON, YAML (`-y`), colour, `--meta-out` metadata | JSON only |
| Selecting part of the input | line slices (`jc 4:15 --df`) | none; `--extract` and `--exclude` keep or drop keys of each record |
| Diagnostics | `-d` for debug output | `--explain` names the definition chosen and why the others were rejected |
| Adding a parser | a Python module in the plugin directory | a YAML definition with captured fixtures, checked by `jz test`; no Go needed |
| Library | Python (`jc.parse('df', text)`) | Go (`pkg/registry`, `pkg/selector`, `pkg/engine`) |
| Runtime | Python 3.6 or later with ruamel.yaml, xmltodict and Pygments, or a prebuilt binary from its release page | a single static binary |
| Integrations | Ansible filter plugin, FortiSOAR connector | none |

The output of the two differs in key names, in which values are converted, and in which texts each accepts. [Coverage](../coverage/#compared-with-jc) has a fixture-by-fixture comparison. It uses jz's own fixtures, which were chosen for jz's definitions, so its counts favour jz and do not measure how often either tool is right on text in general.

## JSON from arguments: jo and jz new

| | jo 1.9 | jz new (v0.8.0) |
|---|---|---|
| Value types | guessed: a number, `true`, `false`, `null`, and a JSON object or array become JSON values; `-s`, `-n`, `-b` force a type per word, and `-B` turns off the `true`/`false`/`null` guess | not guessed: `key=value` is always a string, `key:=json` is always JSON |
| Nesting the output of another call | `a=$(jo b=1)` | `a:=$(jz new b=1)` |
| Booleans | `key@1`, `key@t` | `key:=true` |
| Empty value (`key=`) | `null`, or omitted with `-n` | `""` |
| Nesting by path | `a[b]=1`, or `-d.` for `a.b=1` | `--path /a/b=1` (a JSON Pointer) |
| Arrays | `-a` and `key[]=value` | `--array` and `key[]=value` |
| A file as the value | `@file` text without its final line break, `%file` base64, `:file` JSON | `=@file` text without its final line break, `--text-file` with every line ending kept, `:=@file` JSON or a data file read by its extension; no base64 |
| Standard input | read as words, one per line, or as a document with `-f -` | as a value with `key=@-`, `key:=@-` or `--text-file key=-`; `--each` makes one document per input line |
| Adding to an existing document | `-f file` loads a JSON object or array and adds the words to it | not available; `key:=@file` puts the document under a key |
| The same key twice | written twice; `-D` keeps the last value | refused with exit status 2 |
| Pretty output | `-p` | `-p` |
| Program size, stripped (Linux amd64) | 39 KB, linked to the C library | 15 MB, static, the parsers included |

The difference in value types is the one most likely to matter:

```console
$ jo version=1.10 zip=007 enabled=true note=
{"version":1.1,"zip":7,"enabled":true,"note":null}

$ jz new version=1.10 zip=007 enabled:=true note=
{"version":"1.10","zip":"007","enabled":true,"note":""}
```

jo's guess saves writing `:=`. A value that looks like a number loses its spelling unless its type is forced (`jo -- -s zip=007`).

## Benchmarks

These are the timings of one process per call, measured with [himorime](https://github.com/nao1215/himorime) from `bench/compare/himorime.yaml` and written into this page by `make bench-docs`. They measure time, CPU time and peak memory only, not whether the JSON is correct.

jz is built as the release is built (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`) from the commit named under the tables. jc 1.25.7 is installed from PyPI; its prebuilt binaries were not measured. jo 1.9 is built with meson and stripped.

The `df -h` and `ps aux` input is captured on the machine that runs the suite, and `ps aux` is repeated to 100,000 rows. The CSV has 100,000 generated rows, a third of them with a quoted field that holds a comma. Before measuring, a setup step checks that both tools return the same number of records, and that `jz new` and jo build the same JSON.

jz has three ways to read command output, and they cost different amounts. Detecting the format (`COMMAND | jz`, `jz-detected` below) reads all built-in definitions, because any of them could fit the text. Naming the parser (`--parser NAME`, `jz-parser`) and running the command (`jz run COMMAND`) read only the definitions that answer to that name. jc always reads only the parser it is given.

jz loads its definitions and reads large input on several threads, and jc runs on one. Each case is measured twice: with every core available, and pinned to one core with `taskset -c 0` (the rows ending in "one core"). The pinned figure is closer to what a machine or container limited to one core sees. Starting `taskset` is part of the pinned figures, which is most of the pinned time of jo and `jz new`.

In the tables, Relative is a command's median time divided by that of the jz command its case compares against: `jz-parser` for command output, and the only jz command otherwise. CPU time is user plus system time. Peak RSS is the largest resident set size of the process.

<!-- himorime:begin benchmarks -->

### jz, jc and jo

One process per call on the same input.

#### Latency

| Benchmark | Command | Median | P95 | Mean | Stddev | Min | Max | Runs | Relative |
|---|---|--:|--:|--:|--:|--:|--:|--:|--:|
| start-up | jz | 1.71ms | 3.38ms | 1.90ms | 735.78µs | 963.77µs | 4.29ms | 30 | 1.00x |
| start-up | jc | 35.94ms | 58.28ms | 38.61ms | 10.51ms | 28.44ms | 59.43ms | 30 | 21.05x |
| start-up | jo | 608.47µs | 970.29µs | 636.54µs | 208.95µs | 348.27µs | 1.08ms | 30 | 0.36x |
| start-up, one core | jz | 1.50ms | 2.19ms | 1.54ms | 344.32µs | 948.72µs | 2.56ms | 30 | 1.00x |
| start-up, one core | jc | 29.95ms | 36.57ms | 31.14ms | 3.40ms | 29.28ms | 46.56ms | 30 | 19.99x |
| start-up, one core | jo | 878.11µs | 1.26ms | 909.18µs | 273.59µs | 492.37µs | 1.63ms | 30 | 0.59x |
| df -h | jz-detected | 31.58ms | 34.71ms | 31.87ms | 1.77ms | 28.34ms | 35.42ms | 30 | 5.02x |
| df -h | jz-parser | 6.29ms | 7.26ms | 6.27ms | 604.93µs | 5.26ms | 7.56ms | 30 | 1.00x |
| df -h | jc | 36.23ms | 43.27ms | 37.50ms | 4.23ms | 33.51ms | 53.84ms | 30 | 5.76x |
| df -h, one core | jz-detected | 54.80ms | 106.78ms | 63.22ms | 18.25ms | 52.07ms | 115.30ms | 30 | 9.10x |
| df -h, one core | jz-parser | 6.02ms | 9.77ms | 6.40ms | 1.71ms | 4.62ms | 12.87ms | 30 | 1.00x |
| df -h, one core | jc | 35.04ms | 62.42ms | 39.34ms | 9.73ms | 33.73ms | 68.42ms | 30 | 5.82x |
| running df -h | jz | 7.82ms | 9.36ms | 7.77ms | 965.89µs | 5.61ms | 9.51ms | 30 | 1.00x |
| running df -h | jc | 72.12ms | 77.91ms | 72.43ms | 3.57ms | 65.06ms | 80.37ms | 30 | 9.22x |
| running df -h, one core | jz | 7.27ms | 7.94ms | 7.25ms | 609.08µs | 5.80ms | 8.97ms | 30 | 1.00x |
| running df -h, one core | jc | 67.68ms | 71.14ms | 68.50ms | 4.28ms | 64.89ms | 89.75ms | 30 | 9.32x |
| ps aux, 100k rows | jz-detected | 208.03ms | 237.46ms | 211.22ms | 17.29ms | 197.07ms | 285.80ms | 30 | 1.17x |
| ps aux, 100k rows | jz-parser | 178.47ms | 195.61ms | 183.63ms | 24.66ms | 164.35ms | 307.38ms | 30 | 1.00x |
| ps aux, 100k rows | jc | 436.13ms | 448.34ms | 437.50ms | 7.88ms | 428.12ms | 467.14ms | 30 | 2.44x |
| ps aux, 100k rows, one core | jz-detected | 268.21ms | 324.48ms | 278.04ms | 38.59ms | 260.22ms | 462.16ms | 30 | 1.33x |
| ps aux, 100k rows, one core | jz-parser | 201.08ms | 203.90ms | 207.70ms | 36.95ms | 198.65ms | 403.21ms | 30 | 1.00x |
| ps aux, 100k rows, one core | jc | 439.32ms | 507.85ms | 449.19ms | 35.55ms | 430.70ms | 611.56ms | 30 | 2.18x |
| CSV, 100k rows | jz | 91.79ms | 100.60ms | 92.70ms | 6.62ms | 84.26ms | 119.32ms | 30 | 1.00x |
| CSV, 100k rows | jc | 145.63ms | 151.14ms | 145.35ms | 3.20ms | 140.35ms | 152.25ms | 30 | 1.59x |
| CSV, 100k rows | jz-stream | 109.11ms | 119.81ms | 110.22ms | 5.27ms | 104.31ms | 125.69ms | 30 | 1.19x |
| CSV, 100k rows | jc-stream | 262.34ms | 285.53ms | 268.65ms | 27.58ms | 255.35ms | 409.00ms | 30 | 2.86x |
| CSV, 100k rows, one core | jz | 107.50ms | 108.88ms | 107.62ms | 2.84ms | 100.46ms | 120.20ms | 30 | 1.00x |
| CSV, 100k rows, one core | jc | 142.52ms | 165.98ms | 147.21ms | 14.83ms | 139.59ms | 216.84ms | 30 | 1.33x |
| CSV, 100k rows, one core | jz-stream | 103.99ms | 114.29ms | 105.26ms | 4.18ms | 102.68ms | 121.21ms | 30 | 0.97x |
| CSV, 100k rows, one core | jc-stream | 261.35ms | 272.12ms | 267.76ms | 27.95ms | 257.67ms | 414.31ms | 30 | 2.43x |
| object from arguments | jz-new | 1.15ms | 1.33ms | 1.14ms | 102.52µs | 957.39µs | 1.34ms | 30 | 1.00x |
| object from arguments | jo | 326.64µs | 379.16µs | 327.16µs | 35.19µs | 270.84µs | 389.29µs | 30 | 0.29x |
| object from arguments | jz-new-path | 1.15ms | 1.42ms | 1.16ms | 213.13µs | 805.36µs | 2.03ms | 30 | 1.01x |
| object from arguments | jo-nested | 335.57µs | 487.78µs | 346.83µs | 85.78µs | 202.82µs | 689.85µs | 30 | 0.29x |
| object from arguments, one core | jz-new | 922.29µs | 996.52µs | 921.03µs | 59.63µs | 764.97µs | 1.04ms | 30 | 1.00x |
| object from arguments, one core | jo | 603.42µs | 644.10µs | 593.89µs | 44.56µs | 442.21µs | 667.70µs | 30 | 0.65x |
| object from arguments, one core | jz-new-path | 900.64µs | 1.13ms | 922.00µs | 114.18µs | 734.04µs | 1.33ms | 30 | 0.98x |
| object from arguments, one core | jo-nested | 607.53µs | 655.35µs | 598.37µs | 51.52µs | 443.21µs | 686.75µs | 30 | 0.66x |

Relative is the median divided by the baseline command's median, or by the fastest command's.

#### Throughput

| Benchmark | Command | Median | Mean | Min | P95 |
|---|---|--:|--:|--:|--:|
| ps aux, 100k rows | jz-detected | 480.70k records/s | 475.90k records/s | 349.90k records/s | 506.09k records/s |
| ps aux, 100k rows | jz-parser | 560.31k records/s | 550.81k records/s | 325.33k records/s | 591.14k records/s |
| ps aux, 100k rows | jc | 229.29k records/s | 228.64k records/s | 214.07k records/s | 233.33k records/s |
| ps aux, 100k rows, one core | jz-detected | 372.84k records/s | 364.07k records/s | 216.37k records/s | 378.67k records/s |
| ps aux, 100k rows, one core | jz-parser | 497.32k records/s | 489.33k records/s | 248.01k records/s | 502.15k records/s |
| ps aux, 100k rows, one core | jc | 227.62k records/s | 223.69k records/s | 163.52k records/s | 231.09k records/s |
| CSV, 100k rows | jz | 1.09M records/s | 1.08M records/s | 838.06k records/s | 1.18M records/s |
| CSV, 100k rows | jc | 686.66k records/s | 688.33k records/s | 656.80k records/s | 710.07k records/s |
| CSV, 100k rows | jz-stream | 916.55k records/s | 909.16k records/s | 795.63k records/s | 956.72k records/s |
| CSV, 100k rows | jc-stream | 381.19k records/s | 374.86k records/s | 244.50k records/s | 389.91k records/s |
| CSV, 100k rows, one core | jz | 930.26k records/s | 929.75k records/s | 831.98k records/s | 944.40k records/s |
| CSV, 100k rows, one core | jc | 701.67k records/s | 684.26k records/s | 461.17k records/s | 713.66k records/s |
| CSV, 100k rows, one core | jz-stream | 961.61k records/s | 951.34k records/s | 825.04k records/s | 972.26k records/s |
| CSV, 100k rows, one core | jc-stream | 382.63k records/s | 376.09k records/s | 241.37k records/s | 387.13k records/s |

Throughput is the declared work divided by the latency of each run.

#### CPU

| Benchmark | Command | User | System | Total | Total p95 | Utilization |
|---|---|--:|--:|--:|--:|--:|
| start-up | jz | 866.00µs | 1.27ms | 1.82ms | 2.55ms | 108.1% |
| start-up | jc | 29.57ms | 5.05ms | 35.84ms | 57.18ms | 99.7% |
| start-up | jo | 0ns | 419.00µs | 545.00µs | 853.45µs | 90.8% |
| start-up, one core | jz | 0ns | 1.30ms | 1.44ms | 2.12ms | 96.1% |
| start-up, one core | jc | 26.59ms | 4.51ms | 29.88ms | 36.47ms | 99.7% |
| start-up, one core | jo | 0ns | 501.50µs | 759.00µs | 1.14ms | 93.1% |
| df -h | jz-detected | 156.66ms | 28.22ms | 185.89ms | 205.58ms | 589.1% |
| df -h | jz-parser | 5.42ms | 3.46ms | 9.08ms | 10.64ms | 140.0% |
| df -h | jc | 31.18ms | 5.02ms | 36.05ms | 43.15ms | 99.7% |
| df -h, one core | jz-detected | 49.15ms | 6.02ms | 54.61ms | 105.77ms | 99.8% |
| df -h, one core | jz-parser | 3.45ms | 2.65ms | 5.87ms | 9.20ms | 98.1% |
| df -h, one core | jc | 30.21ms | 5.92ms | 34.80ms | 62.12ms | 99.5% |
| running df -h | jz | 6.07ms | 4.51ms | 10.48ms | 12.13ms | 133.9% |
| running df -h | jc | 60.70ms | 11.79ms | 71.86ms | 77.53ms | 99.5% |
| running df -h, one core | jz | 4.04ms | 2.80ms | 6.92ms | 7.65ms | 95.7% |
| running df -h, one core | jc | 57.44ms | 9.06ms | 66.45ms | 69.76ms | 98.4% |
| ps aux, 100k rows | jz-detected | 402.89ms | 84.55ms | 483.37ms | 534.87ms | 230.6% |
| ps aux, 100k rows | jz-parser | 213.60ms | 56.84ms | 269.22ms | 318.72ms | 146.2% |
| ps aux, 100k rows | jc | 383.85ms | 53.51ms | 435.51ms | 448.06ms | 99.9% |
| ps aux, 100k rows, one core | jz-detected | 214.98ms | 53.98ms | 267.99ms | 323.08ms | 99.9% |
| ps aux, 100k rows, one core | jz-parser | 153.22ms | 47.71ms | 200.89ms | 203.67ms | 99.9% |
| ps aux, 100k rows, one core | jc | 385.36ms | 54.04ms | 439.01ms | 506.15ms | 99.9% |
| CSV, 100k rows | jz | 118.44ms | 23.49ms | 142.58ms | 155.66ms | 154.7% |
| CSV, 100k rows | jc | 121.39ms | 23.90ms | 145.35ms | 150.95ms | 99.9% |
| CSV, 100k rows | jz-stream | 113.49ms | 16.79ms | 130.75ms | 143.20ms | 118.5% |
| CSV, 100k rows | jc-stream | 255.93ms | 7.00ms | 262.12ms | 285.33ms | 99.9% |
| CSV, 100k rows, one core | jz | 88.77ms | 18.14ms | 107.28ms | 108.65ms | 99.8% |
| CSV, 100k rows, one core | jc | 122.36ms | 21.05ms | 142.36ms | 164.56ms | 99.9% |
| CSV, 100k rows, one core | jz-stream | 95.14ms | 8.97ms | 103.84ms | 114.07ms | 99.8% |
| CSV, 100k rows, one core | jc-stream | 254.94ms | 6.99ms | 261.04ms | 271.65ms | 99.9% |
| object from arguments | jz-new | 0ns | 1.22ms | 1.25ms | 1.44ms | 108.4% |
| object from arguments | jo | 277.50µs | 0ns | 299.50µs | 349.55µs | 91.9% |
| object from arguments | jz-new-path | 0ns | 1.16ms | 1.22ms | 1.55ms | 108.8% |
| object from arguments | jo-nested | 301.50µs | 0ns | 306.50µs | 452.55µs | 91.8% |
| object from arguments, one core | jz-new | 0ns | 845.50µs | 879.50µs | 950.95µs | 95.6% |
| object from arguments, one core | jo | 0ns | 544.00µs | 568.00µs | 605.65µs | 94.7% |
| object from arguments, one core | jz-new-path | 0ns | 729.50µs | 866.00µs | 1.08ms | 95.7% |
| object from arguments, one core | jo-nested | 412.00µs | 0ns | 577.50µs | 607.55µs | 94.3% |

CPU values are medians over runs of the process tree. Utilization is CPU time divided by wall-clock time; above 100% means more than one CPU was busy. Process tree: the command plus every descendant its parent waited for (rusage); a descendant left running or reaped by init is not counted.

#### Memory

| Benchmark | Command | Peak RSS (median) | Peak RSS (max) |
|---|---|--:|--:|
| start-up | jz | ≤ 7.57MiB | ≤ 7.57MiB |
| start-up | jc | 19.41MiB | 19.72MiB |
| start-up | jo | ≤ 7.54MiB | ≤ 7.54MiB |
| start-up, one core | jz | ≤ 7.57MiB | ≤ 7.57MiB |
| start-up, one core | jc | 19.70MiB | 19.77MiB |
| start-up, one core | jo | ≤ 7.57MiB | ≤ 7.57MiB |
| df -h | jz-detected | 30.42MiB | 32.09MiB |
| df -h | jz-parser | 14.86MiB | 15.61MiB |
| df -h | jc | 20.95MiB | 21.41MiB |
| df -h, one core | jz-detected | 30.36MiB | 31.86MiB |
| df -h, one core | jz-parser | 14.11MiB | 14.11MiB |
| df -h, one core | jc | 21.39MiB | 21.57MiB |
| running df -h | jz | 15.04MiB | 15.61MiB |
| running df -h | jc | 27.47MiB | 28.10MiB |
| running df -h, one core | jz | 14.36MiB | 14.36MiB |
| running df -h, one core | jc | 28.06MiB | 28.12MiB |
| ps aux, 100k rows | jz-detected | 215.51MiB | 224.36MiB |
| ps aux, 100k rows | jz-parser | 194.89MiB | 205.61MiB |
| ps aux, 100k rows | jc | 198.40MiB | 199.22MiB |
| ps aux, 100k rows, one core | jz-detected | 230.48MiB | 238.86MiB |
| ps aux, 100k rows, one core | jz-parser | 203.11MiB | 207.36MiB |
| ps aux, 100k rows, one core | jc | 216.51MiB | 216.65MiB |
| CSV, 100k rows | jz | 65.87MiB | 68.29MiB |
| CSV, 100k rows | jc | 85.29MiB | 85.83MiB |
| CSV, 100k rows | jz-stream | 11.08MiB | 11.98MiB |
| CSV, 100k rows | jc-stream | 19.77MiB | 20.29MiB |
| CSV, 100k rows, one core | jz | 79.11MiB | 79.36MiB |
| CSV, 100k rows, one core | jc | 85.93MiB | 86.07MiB |
| CSV, 100k rows, one core | jz-stream | 10.35MiB | 10.36MiB |
| CSV, 100k rows, one core | jc-stream | 20.22MiB | 20.29MiB |
| object from arguments | jz-new | ≤ 9.65MiB | ≤ 9.65MiB |
| object from arguments | jo | ≤ 9.63MiB | ≤ 9.63MiB |
| object from arguments | jz-new-path | ≤ 9.64MiB | ≤ 9.64MiB |
| object from arguments | jo-nested | ≤ 9.65MiB | ≤ 9.65MiB |
| object from arguments, one core | jz-new | ≤ 9.61MiB | ≤ 9.61MiB |
| object from arguments, one core | jo | ≤ 9.62MiB | ≤ 9.62MiB |
| object from arguments, one core | jz-new-path | ≤ 9.62MiB | ≤ 9.62MiB |
| object from arguments, one core | jo-nested | ≤ 9.63MiB | ≤ 9.63MiB |

Peak RSS is a resident set size, not the heap size of a language runtime. Peak RSS is the largest peak of any single process of the tree (rusage ru_maxrss), not the combined memory of processes running at the same time. ≤ marks a peak RSS at or below what the process starting the command already used; the command used at most that much.

Measured with himorime v0.1.2 on linux/amd64, AMD RYZEN AI MAX+ 395 w/ Radeon 8060S (32 logical CPUs), head 2a3a868d8e2e, seed 3688326356035415.

- go: go version go1.26.6 linux/amd64
- jc: jc version:  1.25.7
- jo: jo 1.9
- jq: jq-1.8.1
- taskset: taskset from util-linux 2.41.3

<!-- himorime:end benchmarks -->

### Reading the figures

- Starting the process: jz printed its version in about 1.5 to 1.7 ms, jo in about 0.6 to 0.9 ms, and jc in 30 to 36 ms. Most of jc's time on short input is starting Python and loading jc.
- With the parser named, or with jz running the command, jz read a short `df -h` report in about 6 to 8 ms and jc in 35 to 72 ms.
- When jz detects the format of a short output, jc was faster on one core (35 ms against 55 ms) and held less memory (21 MiB against 30 MiB). On all cores jz was slightly faster, using about five times the CPU time: detection reads every built-in definition and compiles the signatures it checks.
- On 100,000 rows of `ps aux` and on a whole CSV, jz was faster than jc on both core settings, detected or named: about 2.2 to 2.4 times for `ps aux` and 1.3 to 1.6 times for the CSV. It held about the same memory as jc for `ps aux` and less for the CSV.
- Streaming a CSV, jz was about 2.5 times as fast as jc on both core settings and held about half the memory.
- jo was about 3.5 times as fast as `jz new` on all cores and about 1.5 times pinned to one core. Both finish in about a millisecond, so the difference matters when a script starts the program thousands of times. Their peak memory is below what this measurement resolves (the rows marked `≤`): both used less than the process that started them.
- These results come from one machine and one run of the suite, and jc's figures depend on the Python version. Run `make bench-docs` to measure another machine.
