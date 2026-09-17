---
title: Compared with jc and jo
description: "What jc, jo and jsonize each do, where they differ, and how long each takes on the same input, measured on one machine."
toc: true
---

Two established tools cover most of what jz does. [jc](https://github.com/kellyjonbrazil/jc) converts the output of commands, file formats and strings to JSON. [jo](https://github.com/jpmens/jo) builds JSON from shell arguments. jz does a part of each: `COMMAND | jz` overlaps with jc, and `jz new` overlaps with jo. This page lists the differences as they stand and does not rank the tools. Each one is the better choice for some jobs.

The versions compared are jc 1.25.7 (released 2026-06-18, MIT licence), jo 1.9 (released 2022-11-04, GPL-2.0-or-later) and jz v0.7.0 (released 2026-09-17, MIT licence). jc has been developed since 2019 and jo since 2016. jz's first commit was in September 2026. jc and jo are packaged by the major Linux distributions and Homebrew. jz is installed with `go install`, a Homebrew tap, the AUR, or the packages on its release page. No code or fixture from either tool is copied into this repository.

## In short

- Use jc when you need a Python library, the Ansible filter, YAML output, line slicing, or input jz does not read: XML, TOML, plist and X.509 files; strings such as URLs, JWTs, timestamps and IP addresses; commands jz has no definition for, such as `traceroute`, `iptables`, `dmidecode`, `wg`, `ufw`, `acpi`, `mdadm`, `ntpq`, `pacman`, `find`, `finger` and `zpool status`; and more `/proc` files. Pinned to one core, jc also read a `df -h` report, a large `ps aux` table and a whole CSV faster than jz in the benchmarks below (only slightly for `ps aux`), and held less memory.
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

| | jc 1.25.7 | jz v0.7.0 |
|---|---|---|
| Choosing the parser | named on every call (`jc --df`), or taken from the command name (`jc df -h`) | detected from the text; `--parser` narrows it, and some generic formats are read only when named |
| What is read | 236 parsers, 19 of them streaming versions of others: commands, `/proc` files, file formats and strings | 737 definitions under 270 names: commands, plus `/etc` and `/proc` files and the generic `csv`, `ini`, `kv` and `table` layouts; no string parsers |
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

| | jo 1.9 | jz new (v0.7.0) |
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

These are the timings of one process per call, measured with `scripts/compare_bench.sh`. They measure time, CPU time and peak memory only, not whether the JSON is correct.

### Environment

- AMD Ryzen AI Max+ 395 (16 cores, 32 threads), Ubuntu 26.04.1, Linux 7.0
- hyperfine 1.20.0, 3 warm-up runs and 60 measured runs each, no shell
- jc 1.25.7 from PyPI in a virtual environment on CPython 3.12.12; jc's prebuilt binaries were not measured
- jo 1.9 built with meson and GCC 15.2, stripped
- jz v0.7.0 built with Go 1.26.6 as the release is built (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`)

The `df -h` and `ps aux` input was captured on the same machine. `ps aux` was repeated to 100,000 rows. The CSV has 100,000 generated rows. Before timing, the script checks that both tools returned the same number of records.

jz loads its definitions and reads large input on several threads, and jc runs on one. Each case was run twice: with every core available, and pinned to one core with `taskset -c 0`. The pinned figure is closer to what a machine or container limited to one core sees. Starting `taskset` adds a fraction of a millisecond, which is most of the pinned figure for jo and `jz new`.

### Time

CPU time is user plus system time.

| Input | Command | All cores | CPU time, all cores | One core |
|---|---|---|---|---|
| `df -h`, 11 rows | `jz` (detected) | 31.1 ms ± 2.2 | 180 ms | 56.0 ms ± 1.5 |
| | `jz --parser df` | 19.0 ms ± 0.7 | 146 ms | 42.9 ms ± 1.2 |
| | `jc --df` | 39.0 ms ± 4.6 | 39 ms | 37.5 ms ± 3.4 |
| `ps aux`, 100,000 rows | `jz` (detected) | 368.2 ms ± 11.3 | 870 ms | 473.7 ms ± 12.5 |
| | `jc --ps` | 464.1 ms ± 8.3 | 464 ms | 460.8 ms ± 5.7 |
| CSV, 100,000 rows | `jz --format csv` | 155.9 ms ± 6.5 | 351 ms | 214.0 ms ± 5.8 |
| | `jc --csv` | 159.0 ms ± 6.7 | 159 ms | 151.6 ms ± 1.8 |
| CSV, 100,000 rows, streamed | `jz --format csv --stream` | 158.9 ms ± 15.9 | 239 ms | 139.6 ms ± 3.5 |
| | `jc --csv-s` | 270.8 ms ± 10.2 | 271 ms | 270.8 ms ± 20.5 |
| object of five values | `jz new` | 1.1 ms ± 0.2 | 1.1 ms | 0.9 ms ± 0.1 |
| | `jo` | 0.3 ms ± 0.1 | 0.2 ms | 0.5 ms ± 0.2 |
| nested object | `jz new --path` | 1.0 ms ± 0.2 | 1.1 ms | 0.9 ms ± 0.1 |
| | `jo -d.` | 0.4 ms ± 0.1 | 0.3 ms | 0.6 ms ± 0.1 |

### Peak memory

Maximum resident set size from GNU time, the median of five runs:

| Input | Command | All cores | One core |
|---|---|---|---|
| `df -h` | `jz` (detected) | 29.9 MiB | 30.3 MiB |
| | `jc --df` | 21.3 MiB | 21.3 MiB |
| `ps aux`, 100,000 rows | `jz` (detected) | 339.4 MiB | 372.0 MiB |
| | `jc --ps` | 237.0 MiB | 238.1 MiB |
| CSV, 100,000 rows | `jz --format csv` | 94.3 MiB | 109.3 MiB |
| | `jc --csv` | 90.3 MiB | 90.5 MiB |
| CSV, 100,000 rows, streamed | `jz --format csv --stream` | 12.4 MiB | 10.0 MiB |
| | `jc --csv-s` | 19.9 MiB | 20.2 MiB |
| object of five values | `jz new` | 5.0 MiB | 4.8 MiB |
| | `jo` | 1.9 MiB | 1.9 MiB |

Repeated runs of the script moved the jz figures for the 100,000-row inputs by up to about 20 MiB, so a difference of a few MiB, as for the whole CSV on all cores, is within that variation.

### Reading the figures

- Pinned to one core, jc was faster than jz for `df -h` and a whole CSV, and slightly faster for `ps aux`. With all 32 threads, jz was faster for `df -h` and `ps aux` and took about the same time for a whole CSV, because it spreads the work over the threads. In each of these cases jz used more CPU time than jc.
- jz held more memory than jc for `df -h` and `ps aux`, more still for `ps aux` when pinned to one core, about the same for a whole CSV on all cores, and more for it on one core.
- Streaming a CSV, jz was faster than jc on both core settings and held less memory.
- Most of jz's time on short input is loading its definitions, about 16 ms on this machine in the `LoadEmbedded` Go benchmark. Detecting the format from the text adds about 12 ms over naming the parser. jc loads only the parser it is given.
- jo was about three times as fast as `jz new` on all cores, 1.5 to 2 times as fast pinned to one core, and held less memory. Both finish in about a millisecond, so the difference matters when a script starts the program thousands of times.
- These results come from one machine and one run of the script, and jc's figures depend on the Python version. Run `RUNS=60 JZ=... JC=... JO=... scripts/compare_bench.sh` to measure another machine.
