# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- `ping/linux` reads what iputils prints besides a clean run (contract
  version 2). A host that did not answer was the parse error it is run
  to find out: the summary then has no round trip times, and the lines
  between header and summary are ICMP errors (`From ... icmp_seq=1
  Destination Host Unreachable`) or, with `-O`, `no answer yet`. Those
  lines are in `replies` with an `error` key, the summary's counts of
  errors, duplicates and corrupted replies, pipe size and inter-packet
  gap are read when printed, an IPv6 header (`56 data bytes`, as
  `ping6` prints) leaves `packet_bytes` null, `-I` fills `source` and
  `device`, `-D` adds a `timestamp` to each reply, and a payload too
  small to time leaves `time_ms` null.
- `systemctl/show` is a list of names and values in the order printed
  instead of one object (contract version 2). A unit with two
  ExecReload= lines, which sshd's has, prints the property twice, and
  the object could hold one of them: `jz run systemctl show ssh` was
  exit 3.
- `jz captured.txt` names a file where a subcommand goes. The refusal
  now says how to read it, with the options it was given:
  `jz --file captured.txt`.
- Input with no text in it, empty or blank lines only, is refused by
  saying so (`unable to identify the input format: it holds no text`)
  instead of that no signature matched it, and a search of one parser
  no longer lists every variant's first expression against it.
- `jz run nice -n 5 df -h` used to answer `did you mean file, ionice,
  mise?`. When the refused command's arguments name a command jz has a
  parser for, the refusal now prints the command line that names it:
  `jz run --parser df -- nice -n 5 df -h`. The same line follows a
  refusal after the run, as with `jz run env A=1 df`, where `env` is a
  parser of its own.
- A conversion that succeeds has read all of its input. Every line is
  read by a parser, joined by `fold`, blank, named by `ignore`, or the
  heading `select.after` states in full; anything else is exit 3 naming
  the lines, instead of JSON that is missing them. Two `dig` replies in
  one capture used to come back as the first one at exit 0. A pattern has
  to reach both ends of the line it reads, a field of type object has to
  cover its whole value, a part's `ignore` hands a line to a sibling
  rather than dropping it, and `--stream` reads past the end of
  `input.select` to report what it left out.
- `--explain` says more and says it one fact per line, each opening with
  `jz: explain: `: the outcome (`chose`, `unidentified`, `ambiguous`,
  `mismatch`, `defined`), the scope the candidates came from (the whole
  registry, `--parser`, the name of the command `jz run` started, the
  path of `--file`, and a path that was dropped), what the chosen
  definition matched, the rule that settled several that fit and each one
  it outranked, the near misses, the definitions held back because they
  are only used when named and how many took no part, and where the lines
  of the input went. `--explain=json` writes the same facts as one JSON
  document on one such line. The text of `--explain` changed with it:
  `jz: df/gnu from embedded` is now `jz: explain: chose df/gnu from
  embedded`, and the rejections are one per line.
- A key printed twice into a kv map or an ini section is exit 3 naming
  both lines. It used to keep the last value and drop the other.
- A drawn table (`split: box`) refuses a line with no bar and no rule on
  it, and a cell past the header's last column; both used to vanish.
- A tree whose `node` pattern names a group `children` is refused when
  the definition is loaded. The node's children were written over the
  value the group read.
- `jz test` changes every fixture — a foreign line at the end and in the
  middle, the whole text twice — and fails a definition that still
  succeeds without the change showing in its result.
- Definitions that dropped text they did not state now read it or state
  it: `dig` reads the command banner, warnings, the EDNS options and the
  question, authority and additional sections; `mtr --report` the host;
  `pactl list` the properties, ports and formats; `upower -i` and
  `upower -d` the device type and the history rows. The headings that
  others start after (`fdisk`, `ip -s link`, `ethtool -k`, `ss -s`,
  `objdump -h`, `netstat`, the sysstat banner, `xrandr`'s Screen line)
  are stated in full, and the legends and summary lines that are left out
  (`dpkg -l`, `systemd-analyze critical-chain`, `tree`) are named by
  `ignore`.

- Loading and validating a registry is measurably slower in the
  benchmarks (13 to 32 percent on `LoadRegistry`, 7 to 12 percent on
  definition validation), which is the definition struct having grown and
  the validator having more branches to walk. Parsing is unchanged.
  End to end the cost is 1.4 percent: converting `df` output through the
  built-in registry went from 78.3 ms to 79.4 ms, and ten of the
  definitions in that registry are new.

- `iostat`, `mpstat` and `pidstat` are read as an array of samples, one
  object per interval, instead of one object for the whole report. Asked
  for an interval those commands print their column header once per
  sample, and the second one reached the table as a row and failed to
  convert, so `iostat 1 3` was exit 3 — the input `iostat` is most often
  asked for could not be read at all. A single-shot run is the same shape
  with one sample in it, which is a breaking change to what those
  definitions return. `--stream` now writes one sample per line.
  The sysstat banner is dropped with it: it names the kernel, the host
  and the CPU count, which `uname -a` and `nproc` also print, and a shape
  with one object for the banner and an array for the rest has no
  streaming form. `vmstat` and `sar` print their header once and every
  sample under it, so they were already one table and are left alone.

### Added

- Every definition has a JSON Schema of its output, derived from the
  definition: keys, types, which keys are always there, which values may
  be null, the values a key can take when the definition names them.
  `jz list --schema COMMAND VARIANT` prints it; the official ones are in
  `registry/schemas`, linked from the parsers page and published at
  `https://nao1215.github.io/jsonize/schemas/COMMAND/VARIANT.json`, each
  with an output contract version (`x-jsonize.version`) that is separate
  from `format`. Every fixture is validated against its schema, and a
  change that could break a program reading the output is refused unless
  it comes with a new contract version: `make registry-update-schema
  BREAKING=command/variant`. CI checks the fixtures with an independent
  validator and compares the schemas with the base branch's.

- The end-to-end suite runs every spec on Windows, verifies the sysstat
  and BusyBox commands on Linux as required rather than when present,
  lists every skipped scenario with its reason and fails on a skip that
  is not the operating system's, verifies the listings that need a
  device on captured output through `jz run`, and pins what `jz run`
  does when its reader leaves, when it is interrupted and when
  `--timeout` passes. `e2e/README.md` says what is guaranteed where.

- `df/portable` and `df/portable-type` read `df -P` and `df -P -T`, the
  POSIX format with `1024-blocks` and `Capacity`, from GNU coreutils and
  BusyBox. It was exit 4.
- `systemd-inhibit/list` reads `systemd-inhibit --list`, one record per
  inhibitor lock, with the operations the lock covers as an array.
- `prlimit/linux` reads the resource limits `prlimit` prints, aligned or
  with `--raw`.
- `powerprofilesctl/list` reads the power profiles, which one is in use,
  and each one's attributes.
- `file/mime` reads `file -i` and `file --mime-type`: the MIME type and,
  when printed, the character set of each file.
- `ip/oneline-link` reads `ip -o link`, one record per interface. It
  was added from the documentation alone, as a test of whether that is
  enough; `ip/link` now states that its header line ends with the group
  and the queue length, and `file/posix` that a kind is a word of its
  own, so neither reads the one-line form. `file/posix` no longer reads
  the MIME types of `file -i` as a kind either.
- `iostat/human` reads `iostat -h`: the percentages as numbers, the
  rounded device figures as the strings printed. `iostat/cpu` used to
  take that report and fail on the percent signs.
- `ip/oneline-address` reads `ip -o address`, one record per address,
  with the words `ip/address` reads under the same names.
- `ls/long-recursive` reads `ls -lR`, and `ls -l` given several
  directories, as one record per directory with its entries. It used to
  pass the signature of `ls/long` and fail on the first heading.

- Thirty definitions for formats that had none: `/etc/services` and
  `/etc/protocols` (also read through `getent`), `nslookup HOST`,
  `aplay -l` (and `arecord -l`), `smartctl --scan`, `systemctl
  list-dependencies --plain`, `systemctl list-jobs`, `blkid -o export`,
  `git worktree list`, `lsusb -t`, `nmcli general status`, `mpstat -I`,
  `gpg --version`, `apt-cache depends`, `apt-cache policy`, `sensors -u`,
  `dpkg --get-selections`, `ip -s -s link`, `systemd-analyze
  critical-chain`, `systemd-analyze security`, `go tool dist list`,
  `journalctl -o short-monotonic`, `curl --version`, `amixer contents`,
  `openssl ciphers -V`, `locale -k`, `tree`, `gh auth status`,
  `mokutil --list-sbat` and `pactl list sinks`. Six of them are trees
  and one is a CSV, which is what the new parse types were for.
- `indent` accepts a list of the forms one level of a tree may take, for
  a report that marks a level with a branch character: `tree(1)` writes a
  level as one of "|   ", "    ", "|-- " and "`-- ", and
  `systemd-analyze critical-chain` as two spaces or a backtick and a
  dash. Each is a fixed width whose characters depend on whether anything
  follows, so a single string cannot count them.
- The duration units gain the week, which systemd prints.
- `parse.type: tree`, for a report whose depth comes from the input:
  `lspci -vv` prints a device, its capabilities under it and a
  capability's flags under those, and how far that goes is a property of
  the machine rather than of the format. The definition states what a
  node is — the fields read from its line, plus a `children` array — and
  the input states nothing but how deep the nodes go. Every line is a
  node, read by `node.parse` (an ordered list of regular expressions, or
  a key/value split), and `indent` states one level of indentation as it
  is written. A line that skips a level, indentation that is not a whole
  number of the unit, and depth beyond 32 are all errors. A tree may be a
  `composite` part and may not be a `records` part. `--stream` writes one
  top-level node per line.
- `lspci/verbose`, `lsusb/verbose` and `iw/dev`, the first definitions to
  use it. `lspci/kernel` and `lsusb/linux` gained one exclusion each,
  because the output they read is a subset of the verbose output and only
  the lines the verbose output adds (the Flags line or register dumps of
  `lspci`, the descriptor blocks of `lsusb`) say which of the two a text
  is.

### Fixed

- A table drawn with a frame and no rule under its header (a headless
  listing, as `psql` prints with a border and no headings) was read as
  a header over several lines, and the answer was `[]` at exit 0. It is
  refused, since the text does not say where a header would end.
- Two outputs of one command in a row were read with the second one's
  header as a row: `docker network ls` twice gave
  `{"network_id":"NETWORK","name":"ID",...}` at exit 0, and so did 20
  definitions with a header line, `csv` and `table/box` among them. A
  line that repeats the header starts a second table and is read as its
  header, so an aligned table cut to other widths the second time is
  cut where its own header says. `readelf -h` read its heading a second
  time as a key; it is left out wherever it stands. `jz test` now checks
  that a list read from the fixture twice is the records of one copy
  twice, which is what finds this.
- A file name that begins with a space lost it at exit 0: `ls -l`,
  `tar -tv`, `unzip -l`, `zipinfo` and `lsattr` took the space for more
  of the gap before the name, and `du` and `wc` trimmed it off both
  ends (`trail ` read as `trail`). One space, or three for `unzip -l`,
  separates the name from what comes before it, and what follows is the
  name. `du -h` had the same trim. `tree` kept its escaped form but dropped the space of a trailing
  `\ `. The checksum listings refused a name that begins with a space;
  they read it.
- `vmstat -w` was exit 4: the wide form draws each group name with
  dashes on both sides (`--procs--`), which the signature did not allow.
  The columns are the same, and `vmstat/linux` and `vmstat/linux-active`
  read it.
- A `split_regex` that can match nothing (`[ \t]*`) split a value between
  every character, so `abc def` read as `["a","b","c","d","e","f"]`. It
  is refused when the definition is loaded.
- An aligned table with a right-aligned value that has a space in it was
  cut inside the value at exit 0: `systemctl list-timers | jz --parser
  table --variant aligned` read `"next": "Fri 2026-09-11 06:55:28 JST
  4min"` and `"left": "27s"`. A cell before the last that holds a tab or
  two spaces in a row, the gap that stands between columns, is now
  refused with the column and the value.
- An aligned table whose header gives one column two words read the row
  wrong at exit 0: `docker ps | jz --parser table --variant aligned`
  split `CONTAINER ID` into two columns, kept the id whole under the
  first and wrote `"id": null`, then carried the `7` of `7 weeks ago`
  into the command. A value that runs past where the next column starts
  and leaves that column empty is now refused with the line, the column
  and the value, since the row does not say whether the column was empty
  or its header word belongs to the one before. A value that pushes the
  rest of the row to the right still reads.
- `tree` printed more than twenty lines was exit 4: the signature asked
  for the closing count, which is past the lines detection looks at.
  `jz run tree -L 2 /etc/apt` is read now, and a short text still has to
  end with the count.
- `jz run systemctl list-units` was exit 4 while any unit had a job
  pending, since systemctl then adds a JOB column. The new variant
  `systemctl/units-jobs` reads that table, with `job` on the unit that has
  one.
- `gpg --list-keys --with-colons` on a keyring with no key prints the
  trust database record alone, and `rustup toolchain list` with nothing
  installed prints `no installed toolchains`. Both were exit 4; they are
  the empty listings they are.
- The hint for a wrapper named an argument that is a directory or a data
  file: `jz run tree -L 2 /etc/apt` suggested
  `jz run --parser apt -- tree ...`. Only something that can be run is
  offered now.
- The examples of the shape definitions piped `docker ps` into
  `table/box` and read `~/.gitconfig` with `ini/default`; `docker ps` is
  not drawn with rules, and a git config that sets a key twice is refused
  by an object that holds one value per key. They use `sqlite3 -box`,
  `kubectl get nodes` and a NetworkManager configuration instead.
- The README, the usage page, `jz run --help` and the design page showed
  `jz run --stream ping` as the example of a stream. `ping/linux` reads
  one object, which has no streaming form, so the example was exit 2.
  They show `vmstat 1` and `iostat 5` now, and the sample of a skipped
  record is one jz prints.
- `jz run --explain` wrote nothing when the command printed nothing:
  `jz run --explain git stash list` answered `[]` with no word of why.
  It now says the outcome was `empty`, which variants the answer was
  judged against, and what the command did. A command that failed or was
  ended before it finished is explained as well.
- `ls -ld /etc | jz` was ambiguous, and `jz --parser etc` read the same
  line as a protocol named `drwxr-xr-x` with the number 162 at exit 0.
  `etc/protocols` took any word after the number for the protocol's
  upper-case alias; it now asks for one that opens with a capital, as
  every entry in the file has.
- `vmstat -t` and `vmstat -a -t` were exit 3: the date and time `-t`
  adds after the last column ran into it. Each sample now carries a
  `timestamp`, as printed, since the zone is named only by the
  abbreviation at the head of the column.
- `ls -l --si` was exit 3: `--si` writes the kilo step as a lower-case
  `k` (`4.1k`), which the size in every `ls` definition left out, along
  with the exa and larger steps. The sizes stay as printed.
- A command that ended and left a process behind holding its output
  open, such as a script that starts something in the background, was
  exit 1 with `WaitDelay expired before I/O complete` and nothing
  converted; with `--stream`, jz waited for the process it left, which
  for a daemon is for ever. What the command printed before it ended is
  now read, three seconds after it ended at most, and standard error
  says a process it started still held the output.
- An option that takes one value, given two, kept the last without a
  word: `jz -f a.txt -f b.txt` converted `b.txt` at status 0 and said
  nothing about `a.txt`, and `--parser`, `--variant`, `--define`,
  `--assume-year` and `--timeout` did the same. A second value is now a
  usage error (exit 2). `--extract`, `--exclude`, `--assume-zone` and
  `--env` take one value each time and may still be repeated.
- `jz < /dev/null` printed the help on standard output with status 0.
  /dev/null is a character device, as a terminal is, and jz took any
  character device for the terminal it prints its help to. It now asks
  whether standard input is a terminal, so cron, a CI step or `ssh -n`,
  which leave /dev/null there, get the refusal of empty input (exit 4).
- A blank line after the last line of the input made 44 definitions
  unidentified: `cat /proc/meminfo; echo` was exit 4. A signature that
  says every line has one shape ends at `\z`, the end of the lines it
  looks at, and the blank line was one of them though the parser skips
  it. A blank line before the first line did the same to 507 of the 743
  fixtures, through signatures anchored at `\A`. Blank lines before and
  after the text are no longer part of what a signature sees, and the
  ones before it are skipped by `skip_blank: false` definitions too.
  `jz test` and `make registry-test` now require every fixture to give
  the same answer with CRLF line endings, a byte order mark, no line
  break at the end and a blank line before or after it.
- `lsblk -f -r` came back at exit 0 with its values under the wrong keys:
  `-r` prints the header of the aligned table one space apart and the
  rows unaligned, and cutting the rows where those header words start
  put the filesystem version under `fsver` together with the type, the
  use percentage under `uuid` and the mount point under `fsavail`. The
  util-linux table definitions (lsblk, findmnt --df, losetup, lsfd,
  lsipc, lslocks, lslogins, lsns, uuidparse) now ask for a header that
  is lined up, and refuse the raw form; the others used to fail on it by
  chance, at exit 3.
- An aligned table cut a row holding CJK characters or kana one
  character late for each of them, and wrote the result at exit 0:
  `systemd-inhibit --list` gave an inhibitor named `スクリーンロッカー`
  the user id and user name too, and moved every value after it one key
  to the right. Positions are now counted in terminal columns, the way
  the table was lined up.
- `jz run` answered a command that printed nothing with `[]` whatever
  its arguments were, including arguments under which no definition
  reads its output: `jz run systemd-inhibit --what=sleep true` ran a
  wrapper, and the empty list claimed there were no locks. The
  arguments now choose among the definitions the same way they would
  with output, and with none left the answer is exit 4.
- A `float` field read Go's own number syntax, so `1_000.5` became
  1000.5 and `0x1p3` became 8; `int` already refused both. The days of a
  `duration` took any number too, `1_0-01:02:03` as ten days and
  `1e3-00:00:00` as a thousand. Both now take digits only.
- A command `jz run` stopped at `--timeout`, or one ended by a signal,
  had the line it was writing when it stopped read as a whole record:
  `du` cut off in the middle of a path gave that path shortened. With
  `--stream` that record is now left out and the ones before it stay;
  without `--stream` nothing is written, since the output never came to
  its end. The status is 128 plus the signal, as before.
- `stat` of a device file was exit 3: the definition looked for the
  device numbers on a line of their own, and GNU, uutils and BusyBox all
  print them at the end of the Device line. They are read as
  `device_type`, and the SELinux label as `context`; the label used to be
  matched and left out.
- `jz run lspci -v` was exit 3. The output of a single `-v` met the
  signature of `lspci/kernel`, which then failed on the lines `-k` does
  not print. It now lands on `lspci/verbose`, which reads `-v` and `-vv`
  alike.
- An option that takes a value, given an empty one (`--define ""`,
  `--parser ""`, `--file ""`), counted as not given, so a script passing
  an unset variable had its input read by whatever detection chose. It
  is a usage error now. `jz version` refuses arguments as `jz list` does,
  and `jz test` says a directory does not exist rather than that it has
  no `parsers/` in it.
- `ss -a` and `ss -l` were exit 3 wherever they list netlink or packet
  sockets, whose peer is a bare `*`. Those rows are read now, with the
  protocol and the process or interface as the local address and port,
  and no `peer_port` (the `ss/linux` and `ss/single-protocol` schemas
  are version 2).
- `iostat -m` read the megabyte columns as strings where the kilobyte
  ones are numbers. They are numbers now in `iostat/linux`,
  `iostat/device` and `iostat/extended`, whose schemas are version 2.
- `df -a` was exit 3: an automount point nothing has mounted yet prints
  a dash in every figure. `df/gnu`, `df/gnu-type` and `df/gnu-inodes`
  read the dash as null, which their schemas now allow (version 2).
- `mount -l` was exit 3 on a file system with a label. The label is
  read as `label`.
- Output written with `-z` or `--zero` (`git ls-files -s -z`,
  `git log --oneline -z`, `sha256sum -z`, `ls --zero`) came back at exit
  0 as one record whose last field held every other record. A format
  read line by line now refuses text holding a NUL byte, detected or
  named, and the message says what the text most likely is.
- Options that add to the end of a line came back at exit 0 inside the
  last field. `ar tvO` and `nm -l` are read now (`offset`, `location`),
  as is what `ip -d rule` prints after the table (`attributes`; a rule
  with neither a table nor an action is refused rather than read whole
  into `selectors`). `git ls-files -s --eol` and `fc-list` with element
  names are refused by the text. `tree -F` and the tree options that put
  metadata before a name, `git log --oneline --decorate`, `--parents`
  and `--children`, `git branch -vv` and `cksum -a sysv` are refused
  under `jz run`.
- `jz run wc -lwcL` came back at exit 0 with the fourth count at the
  start of `filename` ("6 /etc/hostname"). `-L` and `-m` are now
  excluded; the comment already said `-m` output must not be read.
- `ls -lF` and `ls -lp` came back at exit 0 with the mark `-F` adds in
  `filename` ("docs/", "link@"). The long listings now exclude those
  arguments under `jz run`, and refuse a name that ends with "/", which
  no name can, so the same listing piped in is refused too. A variant
  that excludes an argument is no longer offered as the one to name.
- `ss -e` and `ss -o` came back at exit 0 with the socket's uid, inode,
  cgroup and timers under `process`, padded with the spaces ss aligns its
  columns with. `process` is now the `users:((...))` list of `-p` and
  nothing else, and a line carrying the other fields is refused.
- The message for text jz could not identify told the reader to name a
  parser (`--parser df`, or the first variant of the command), and a named
  parser is held to its signature, so following it was refused the same
  way. It now names the variant the system or the arguments ruled out
  when there is one, and otherwise the shapes read by name (`csv`, `ini`,
  `kv`, `table`) and `--define`.
- `sar` on a day the machine rebooted was exit 3, in every report. The
  daily file then holds a LINUX RESTART line and a second run with its
  own column names and Average rows; the line is now left out like the
  banner, and the rows of both runs are read. The banner is stated in
  full rather than as any line that opens with "Linux ".
- Some patterns matched values outside any named group, so the line
  counted as read and the value was missing from the result: the server
  name and transport on `dig`'s SERVER line (now `server_name` and
  `protocol`; the `dig/bind` schema is version 2) and the name of the
  programming interface that `lspci -v` prints (`prog_if_name`).
- `--stream` on a `records` format with a record that could not be read
  lost the start of the next record as well, and reported the rest of it
  as text before the first record.
- `jz run --stream` waited for the command to end after the stream had
  failed for a reason of its own, such as `--extract` naming a key the
  format does not produce, and a command that never ends made that
  forever. The command is stopped instead, the failure is what jz
  returns, and the stop is not reported as the command's own status.

- A reader that had gone, as `head` does once it has seen enough, ended
  jz by SIGPIPE before it could stop the command it had started, so
  `jz run --stream ping host | head -3` left `ping` running until its
  next write failed. jz now keeps going long enough to stop the command
  and ends with the same 141.

- Ending the command jz started waited for whatever that command had
  left behind holding its standard output: `jz run --stream sh -c
  'ping host'` interrupted did not return until `ping` did. Waiting for
  the command is now waiting for the command alone.

- jz reading a pipe ignored an interrupt until its input ended: the
  subscription that forwards signals to a command was made at start-up
  whether or not there was a command. It is made only while one runs,
  so `producer | jz` told to stop stops.

- A file's path could name a definition that describes a shape, and those
  carry no signature, so nothing about the text could rule one out: `df`
  output in a file called `comma` inside a directory called `csv` was
  read as CSV and returned at exit 0, where the same bytes on standard
  input were read as `df`. Wrong JSON at a successful exit is the one
  failure this tool exists to prevent. A path now names only a definition
  that makes a claim the text can refuse.
- A path that named a definition whose signature accepted the text but
  whose parse then failed gave exit 3, where the same bytes on standard
  input were read correctly. The fallback ran only when the choice
  failed, not when the reading did, so the guarantee that a path can only
  add an answer was not kept. Both failures now drop the path.

- Checking a registry took 25 seconds where it had taken one, as soon as
  a deeply nested fixture was added. go-cmp builds its report as it walks
  a value, and on a 22-level tree that runs for tens of seconds whether
  the values agree or not. The comparison is now made first and described
  only when it fails, and the description is by line rather than by
  structure: a golden mismatch is two pretty-printed documents, and where
  they part company is what a reader wants.
- `ip route` read the type a route may open with as the destination.
  The main table prints none, because every route in it is unicast;
  `ip route show table all` prints the local table too, whose routes are
  local, broadcast, anycast and multicast, and every one of them failed.
  The list of types is the one the kernel has rather than the ones seen
  so far: the first pass took it from one machine's IPv4 table and missed
  anycast, which is printed only for IPv6.
- `ip -s -s link` was claimed by the definition for one `-s` and then
  failed on the error tables the second one adds. The two are now
  separate definitions, and each refuses the other's output.
- `ip link`, `ip -s link` and `ip address` refused an interface that
  carries a free-text alias, which is a line `ip` prints between the link
  layer address and the alternative names. `ip link` read it as one more
  alternative name and failed; `ip -s link` counted lines to find its
  counter tables, and the extra line moved them. The tables are now found
  by their header, the alias is a part of its own, and each of the two
  parts under the link line names the other's lines as belonging to a
  sibling rather than taking a position, since which of them comes first
  is not something the format promises.

- The streaming reader trimmed the carriage return of a CRLF line ending
  before stripping escape sequences, where the whole-document reader
  strips first. A record that ended with an escape sequence around the
  carriage return was read differently by the two.

- `--define YAML`, which takes the definition itself instead of the name
  of one: a `parser.yaml` without `format`, `command`, `variant` or
  `detect`, so what is left is `parse` and optionally `input` and
  `fields`. Nothing is detected, so it cannot be combined with `--parser`
  or `--variant`. It adds no expressive power — the body goes through the
  same loader and validation as a file — so a definition worth keeping
  moves into a registry unchanged.
- Definitions that describe a shape rather than a command:
  `table/whitespace`, `table/aligned`, `table/box`, `csv/comma`,
  `csv/tab`, `kv/colon`, `kv/equals` and `ini/default`. They are
  `--define` under a name and are never reached by automatic detection.
  Such a definition makes no claim about the text, so the conformance
  runner leaves it out of the check that asks whether a definition reads
  a neighbour's output, and requires the variant to be named rather than
  the parser alone.
- `parse.type: csv`, with RFC 4180 quoting and a `delimiter` (default
  `,`), so a value may contain the delimiter, a quote written twice or a
  line break. A short row leaves the remaining keys null and a long one
  is an error; a header naming a column twice numbers the repeats.
- `parse.type: ini`, reading `[section]` headings and `key = value`
  lines into an object of objects. Keys before the first heading go under
  the empty name, `#` and `;` start a comment only at the start of a
  line, a repeated section continues the first and a repeated key takes
  the last value.
- `table.split: box`, for a table drawn with `+-=~` or any character of
  the Unicode Box Drawing block. The bars mark the cells, and the rules
  separate the header from the body: inside the body a line is a row of
  its own, which is what MySQL, psql, `sqlite3` in box mode and `duf`
  print, and a line whose first cell is empty continues the row above it,
  which is how a table that wraps a long value writes the rest. A header
  written over two lines is one name joined with `_`, and an empty cell
  is null.

### Fixed

- `StripANSI` would take a line break as the final byte of an escape
  sequence and eat it, joining two records into one. It did so only when
  the whole document was read, since the streaming reader has already cut
  the line off, so the two readings disagreed on text as small as
  `"\x1b\n"`. No escape sequence ends with a line break or a carriage
  return, so none may consume one. Found by the fuzz target added for the
  new parse types.
- The streaming reader checked that a record was valid UTF-8 before
  stripping its escape sequences, where the whole-document reader strips
  first. A record whose only invalid bytes were inside an escape about to
  be removed was refused by one reading and accepted by the other.

- `fields.<name>.type: duration`, which turns a printed length of time
  into seconds: `3-04:05:06`, `04:05:06`, `04:05`, `01:23.45`, `13:42m`,
  `13 days, 4:30`, `45 min`, `3days`, `1h2m3s`, `3d4h`. The result is an
  integer when the length is a whole number of seconds. `layout` is
  required and is `h:mm` or `mm:ss`, saying what the last part of a bare
  two-part reading is, since `ps` prints four minutes fifty seconds as
  `4:50` and `uptime` prints an hour and twenty-three minutes as `1:23`.
  Applied to the TIME column of every `ps` variant and to the uptime of
  `uptime`, `w` and `top`, whose expected JSON changed from a string to a
  number. `top`'s TIME+ stays text: it falls back from `mmm:ss.hh` to
  `hhh,mm` for a long-running task, so the column would carry two
  different precisions.
- `--assume-year YEAR` (or `now`) and `--assume-zone ABBR=+HHMM`
  (repeatable), which supply what a format leaves out. A time field whose
  layout carries no year now writes `year: assumed` beside it, which says
  the format prints none; the value stays the string it was printed as
  until `--assume-year` says which year to read it in. A layout naming
  its zone with `MST` converts when the abbreviation states its own
  offset (`UTC`, `GMT`) and otherwise waits for `--assume-zone`. Applied
  to `who`, `last`, `journalctl -o short`, `date` and `timedatectl`.
  Nothing reads a clock or a zone database on the way past: `now` is
  resolved once when the command line is read, and whatever Go made of an
  abbreviation is discarded, since Go resolves one against the running
  machine and the same text would convert differently on two machines.
- `date` reports the whole reading as `timestamp` beside the parts it is
  made of.

- `--raw`, which skips the field rules so that every value is the text it
  was extracted from. A value that comes out wrong was either cut from
  the wrong place or converted wrongly, and once it is typed the two look
  alike; this separates them. Nothing is converted, `trim_prefix` and
  `null_if` are not applied, and `required` and `when_missing` are left
  out with them, so the shape does not change with the values in it. It
  is not offered on `jz test`, which compares a fixture against the typed
  JSON beside it. The option existed before release and was removed as a
  second output shape for a consumer to choose between; it is back for the
  other reader, the one writing the definition.

- `--explain`, on every mode, which writes the chosen definition and what
  it was chosen on to standard error: the definition and the registry it
  came from, the conditions the input met, and the definitions that were
  left out with the reason for each. Standard output and the exit status
  are untouched, and no clock reading is written, so two runs over the
  same input explain themselves identically. A search of the whole
  registry lists only the definitions that came close, since rejecting
  three hundred others on the first expression of a signature explains
  nothing; a search scoped to one parser lists every variant.
- A file path is read as evidence about the text in it: the directory
  names the parser and the file names the variant, and the pair has to
  exist in the registry. `jz --file /etc/fstab` and
  `jz --file /proc/meminfo` now convert without the parser being named,
  which is what the formats too unremarkable to claim on sight needed.
  Nothing in the code knows about `/etc` or `/proc`; those work because
  parsers of those names exist. The path stays evidence: a definition it
  names that the text does not fit is dropped and the text is read on its
  own terms, so a path can only add an answer.

- `--stream` reports a record it cannot read and carries on with the next
  one, instead of ending there. The line is named on standard error as
  `jz: <definition>: line N: <reason>`, the records after it are still
  written, and the status is 3 at the end if anything was skipped. This is
  what the commands `--stream` exists for actually print: `ping` puts a
  request timeout among its replies and `rsync` puts progress among its
  file names, and ending at the first of those threw away everything still
  to come. Reading a whole document is unchanged — one record that does
  not fit still means the text is not the format it claimed to be, so
  nothing is written. `engine.Stream` takes an
  `onError func(*ParseError) error` beside `emit` so a library caller
  chooses between the two.

- A decoy corpus under `registry/testdata/decoys/`, checked by
  `make registry-test`. It holds text that belongs to no format jz reads
  and every definition is applied to every file of it. The machinery
  already existed for third parties as `jz test --decoys DIR`; what was
  missing was a corpus of its own for the official registry, so the texts
  that had defeated a signature once lived in a scratch directory and
  were checked by hand. The first thing it caught on being wired in was
  `etc/crontab` reading a shell script.
- Automatic detection as the default behaviour: `COMMAND | jz` and
  `jz < FILE` identify the format from the text and convert it.
- `jz run COMMAND` for the case where jz starts the command itself,
  passing standard input through, forwarding standard error and mirroring
  the exit status.
- `jz list`, `jz list COMMAND` and `jz list COMMAND VARIANT`, with
  `--json` and `--sources`.
- YAML parser definitions (format 1) with table, regex, key/value and
  composite parsers, ordered regex alternatives, NUL-separated records and
  typed field conversion.
- Layered registries: `JSONIZE_REGISTRY_PATH`, the user directory and the
  registry embedded in the binary. jz makes no network access.
- `--extract KEY` and `--exclude KEY`, repeatable, which keep or drop the
  named keys of every object jz prints. Naming a key the format does not
  produce is an error listing the keys it has, and the two options cannot
  be combined.
- `--stream`, on the default mode and on `jz run`, for a command that
  keeps printing: one JSON document per line, each written as soon as the
  record behind it is complete. It applies to the formats that yield
  records (`table`, `regex` per line, a kv list, `records`); a format read
  into one object is refused, as is `--pretty`. This is the only option
  that changes the output contract, from "standard output carries a
  complete document or nothing" to "every line of standard output is a
  complete document".
- `/proc/meminfo`, `/proc/loadavg`, `/proc/uptime`, `/proc/cpuinfo` and
  `/proc/diskstats`, as variants of a parser named `proc`, read from a
  pipe or a redirect the way the `/etc` files are. All but one say enough
  about themselves to be identified from their text; `/proc/uptime` is
  two decimal numbers and nothing else, so it is named rather than
  claimed on sight.
- `/proc/cpuinfo` is `cpuinfo-x86`, because the file has no shape common
  to the architectures: arm64 writes `Features` and `CPU implementer`
  where x86 writes `flags` and `vendor_id`. The variant name is what
  says which one jz reads, and an arm64 machine gets exit 4 rather than
  a block with most of its fields missing.
- `/proc/diskstats` is read for the twenty-field row Linux 5.5 and later
  write. The fourteen- and eighteen-field rows older kernels write are
  refused as rows with too few fields, so nothing shifts and no counter
  is filled in. Those shapes are not covered because no machine running
  such a kernel was available to capture from; the format could describe
  them, and whether to do that with one definition or a variant per shape
  is an open question in design.md.
- `parse: type: records` for a report whose blocks repeat: a line
  matching `start` opens a record and the `parts` are applied to each. A
  `composite` part may itself be `records`, which is what a report that
  opens with a header block and then repeats a block needs.
- `aliases:` on a definition, for the commands that print a format
  another command prints: `vdir` prints what `ls -l` prints, `printenv`
  what `env` prints, `getent passwd` what `/etc/passwd` holds, `podman`
  and `nerdctl` what `docker` prints, and the g-prefixed coreutils macOS
  installs print what the coreutils commands print. `jz run`,
  `--parser` and `jz list` all take the other name.
- `type: time` for a field, with `layout` (a Go reference layout,
  required, and it has to state a year) and `location` (`utc` by default,
  or `local`). The value is written as an RFC 3339 string and never as an
  epoch number. It is applied where the text says what it means:
  `journalctl -o short-iso`, `stat`, `mtr --report` and `date -R` all
  print a numeric offset. A timestamp with no year (`who`, `last`,
  `journalctl -o short`), one with only a zone abbreviation (`date`,
  `systemctl list-timers`, `journalctl --list-boots`, `timedatectl`) and
  one with no zone at all (`tar -tv`, `unzip -l`) stay the strings they
  were printed as, because dating them means inventing what the output
  does not say.
- `input: fold:` joins a value that continues on the next line onto the
  line it belongs to, for the reports that break a long value at the
  terminal width (`ethtool`) and for the control files whose values
  continue on an indented line (`dpkg -s`, `apt show`).
- Official definitions for 160 commands in 355 output formats, covering
  coreutils and procps, the util-linux listings, the systemd tools, the
  classic network commands and the iproute2 ones, sysstat, the disk
  layout tools, the hardware and display tools, the archive and checksum
  tools, the account and firmware listings, the sound and font listings,
  the binary inspection tools (`nm`, `objdump`, `size`, `ar`, and the
  three hex dumps `xxd`, `hexdump -C` and `od`), the process attributes
  (`taskset`, `chrt`, `ionice`, `capsh`, `getcap`, `namei`), git
  including its plumbing, openssl, docker, the package managers, the
  language toolchains, and two dozen files under `/proc` read the way the
  `/etc` files are, plus a handful that describe a shape rather than a
  command (`table`, `csv`, `kv`, `ini`). `jz list` prints the current set.

### Fixed

- The suggestion for a parser name that does not exist offered every
  command within two edits, alphabetically, capped at three. At a few
  hundred commands that buries the one the caller meant and can drop it
  entirely: `--parser dg` answered "did you mean ar, df, dig", where `ar`
  is two edits away and the other two are one. Only the closest are
  offered now, and a name that extends a command is ranked by the same
  distance as everything else, so `--parser lscpuu` says lscpu rather
  than lscpu and ls together.
- `git/log-oneline` read a symbol table. `nm -D` prints a zero padded
  sixty-four bit address, a one letter symbol type and a name, which is a
  hexadecimal word followed by a line of text; the definition claimed
  seven to twenty hexadecimal digits and so reported 0000000000000000 as
  a commit hash. The bound is fifteen digits now, which is past what any
  repository's abbreviation reaches and short of where an address column
  starts.
- `file/posix` read a dump of a file's bytes. An xxd line is a word, a
  colon and a line of text, and any dump has some line whose printable
  column spells one of the kinds file(1) names, so a hex dump was read as
  a listing of sixteen byte long paths. Every description must name a
  kind now, or be one of the two messages file(1) writes for a file it
  could not identify; one line naming a kind is still required as well,
  which is what keeps a listing of nothing but errors refused.
- `etc/crontab` accepted a shell script that installs a cron entry. One
  entry is not evidence: a script that installs one carries one, and so
  does a document that quotes one. The signature now claims the whole
  text, which is entries, variable assignments, comments and blank lines
  and nothing else, so the refusal happens before parsing rather than on
  the first line that is not an entry.
- `git log --stat`, `--numstat`, `--shortstat`, `--name-only`,
  `--name-status` and `-p` were read as `git/log` and their file lists,
  diffstats and diffs were reported as lines of the commit message. The
  message part took whatever followed the blank line; it now requires
  what git actually prints, which is a message indented by four spaces.
  None of those six formats is read: a diffstat elides its paths and pads
  its columns, so there is nothing to recover the file names from. This
  is not something a signature could have settled, because the leading
  lines of such a listing are an ordinary commit block.
- `etc/crontab` read the other lines of a shell script that installs a
  cron entry. A signature only has to find one entry to vouch for a file,
  and the five time fields of the parse pattern took any word, so
  `echo "installed ..."` became an entry with `echo` as its minute. The
  time fields now repeat the vocabulary the signature checked for.
- `ethtool` and `mtr` signatures that looked for their opening line
  alone, which prose explaining the format carries as a quotation. Each
  now also requires the thing the report is made of: an indented property
  under `Settings for eth0:`, and a hop line under the `HOST:` header.
- Signatures that accepted the output of a neighbouring command. `group`
  read `/etc/passwd`, `uname -a` and `timedatectl` output; `hosts` read
  the clock at the start of BSD `uptime` and `w` output as an IPv6
  address; `env` claimed `/etc/os-release` under automatic detection and
  kept the shell quotes inside the values; `file` read `blkid` output and
  any `token:` line followed by a newline; `ip -brief address` read
  `ip -brief link` output, putting the MAC address in the address list.
- The same failure in the definitions added later, each found by
  applying every definition to every other definition's fixtures.
  `cksum` read a crontab entry as a checksum and a size; `etc/passwd`
  read the first line of BusyBox `ifconfig` and a `gpg --with-colons`
  key; `etc/fstab` read a mount table header as an entry; `file` read
  `udevadm`, `ip rule`, `bridge link`, `nmcli` and Debian control
  records; `iostat` read the extended device table with the definition
  for the CPU one; `etc/crontab` read a user crontab, whose command
  lands where the user name belongs.
- The sysstat reports asked for with an interval. `mpstat`, `pidstat`
  and `iostat` had only ever been captured without one, so none of them
  had seen the summary block an interval adds, which prints the column
  names again and labels its rows Average.
- Definitions that listed arguments their command does not need. `swapon`
  and `systemctl` print with no arguments exactly what their definition
  reads, and both turned that call away; the `ls` list enumerated flag
  combinations that bundling already covers and left out the long
  spellings. `lsattr` refused the relative path it prints when given no
  operand, `ss` had no pattern for the unix sockets a bare call lists,
  and `systemctl` on current systemd closes with a legend the ignore list
  did not cover.

### Changed

- `env` is read only when the parser is named. `NAME=value` is the shape
  of a `.env` file, a properties file and a shell fragment too, and every
  one of those that came along wanted another exclusion inside `env`.
- `passwd`, `group`, `hosts` and `os-release` describe files rather than
  commands and are now variants of `etc`. Only `passwd` also existed as a
  binary, and `jz run passwd` used to start the interactive one.
  `/etc/os-release` no longer needs a name: `PRETTY_NAME` together with
  `NAME` and `ID` is that file and close to nothing else.
- `jz run` answers a command that succeeded without printing anything
  with `[]`, where the format it named has an empty form.
- A kv definition can convert the values of labels that are not
  identifiers (`CPU(s)`, `Thread(s) per core`).
- `expect_error` in a fixture covers a definition refusing text at
  detection, not only at parse time.
- `internal/registry`, `internal/definition`, `internal/selector`,
  `internal/engine`, `internal/convert` and `internal/jsonutil` moved to
  `pkg/`, so another program can load a registry, select a definition and
  parse with it. `internal/cli`, `internal/runner`,
  `internal/conformance` and `internal/buildinfo` stay internal: they are
  decisions about a command line, not about reading text.

### Removed

- `min_jsonize`, `metadata.fixtures`, `metadata.authors` and the kv
  `key_name`/`value_name` pair. No definition used any of them, and
  nothing read them back: `authors` was documented in the definition
  format and reported by neither `jz list` nor `jz list --json`.
- `parse.on_mismatch`. `skip` dropped every line a pattern did not fit
  without saying which, which is the failure jz exists to prevent one
  line at a time: `host` was silently discarding the HTTPS record in its
  own fixture. A composite part now names the lines of its region that
  belong to a sibling with `ignore`, and a line nothing claims is an
  error.
- The `size` field type, its `unit` key and `convert.Size`. No
  definition used it: a size printed with a unit is rounded, and turning
  `1.8T` into bytes writes a number the command never printed. It also
  lost precision on a plain count above 2^53, turning
  `9007199254740993` into `9007199254740992`. A plain count is an `int`.

### Decisions worth knowing

- A signature says what a format is, not what it is not. `none` is a
  safety valve for the case where there is no positive form to write,
  with the reason beside it, rather than the way a definition keeps its
  neighbours out. A list of what a format is not cannot be finished, it
  grows by one with every definition somebody adds, and it couples the
  two together: `file/posix` had collected fourteen such expressions and
  now has none. Of the 68 the registry carried, 31 excluded nothing at
  all, 13 named another command, and 11 remain, every one telling
  variants of a single command apart.
- Adding a definition needs nothing but its own signature and its own
  fixtures. Needing to edit somebody else's definition means the two
  were coupled through an exclusion, and `jz test` is what shows it.
- A signature may narrow what a definition undertakes to read, and that
  is a decision to write down. `git log --oneline` is read for an
  abbreviated hash of seven to twenty digits, `cksum` for a checksum
  written in full, and `file` for the kinds of file named in its
  signature. What falls outside is refused with exit 4, which is an
  answer; the failure to design against is text read confidently with
  the wrong definition and returned with status 0.
- Fixtures verify a signature; they do not decide it. The order is
  decide the format, write the signature, capture fixtures that show it.

- A rounded, human-readable size (`df -h`, `free -h`, `ls -lh`, `lsblk`)
  is reported exactly as printed. The base is not recoverable from the
  output and the value is rounded, so a byte count would be invented.
- A format too generic to recognise (`du`, `wc`) is only used when the
  parser is named; its signature is still checked then.
- Naming a parser or variant never disables the signature check.
- The public options are `--file`, `--pretty`, `--parser`, `--variant`
  and `--help`. The input limit is 64 MiB and is not configurable.
- Two commands that print the same format are one definition with an
  alias, not two definitions. Nothing in the text says which of them
  wrote it, so a second definition would read the first one's output and
  the answer would be a guess about the command rather than a reading of
  the text. The alias carries the arguments its name needs, which is how
  `getent passwd` and `getent group` reach different files.
- Format 1 is not frozen until the first tag. After it, adding a key is a
  minor release and the number stays 1; an older jz refuses a definition
  that uses a new key with an unknown-key error saying a newer jz may be
  needed. Removing a key or changing what one means is format 2, because
  neither can be told from a mistake by looking at the file.
- A tree of arbitrary depth has no shape jz can promise in advance and
  is left out: `npm ls --all`, `iw dev`, `lsusb -t`, `docker info`,
  `apt-cache policy`, `systemd-analyze critical-chain`. Most of those
  commands print JSON of their own.
