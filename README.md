# jsonize

**jsonize — Turn command output into JSON.**

`jz` runs a command (or reads output you already have) and converts the
human-oriented text into machine-friendly JSON. Parsers are YAML
definitions in a registry, not Go code: adding support for a command, an
operating system's variant of it, or a new option is a pull request that
contains only YAML and fixtures.

```console
$ jz run df -h
[{"filesystem":"/dev/nvme0n1p2","size":1979120929997,"used":1759218604442,"available":155692564480,"use_percent":92,"mounted_on":"/"},...]

$ df | jz parse df | jq '.[] | select(.use_percent > 90) | .mounted_on'
"/"

$ jz run --pretty uptime
{
  "time": "13:57:18",
  "uptime": "13 days,  4:37",
  "users": 5,
  "load_1m": 0.43,
  "load_5m": 0.65,
  "load_15m": 0.76
}
```

## Install

```console
# from source (Go 1.26 or later)
go install github.com/nao1215/jsonize/cmd/jz@latest

# from a release archive
curl -sSfL https://github.com/nao1215/jsonize/releases/latest/download/jsonize_<version>_linux_amd64.tar.gz | tar xz
```

Release archives ship a `checksums.txt`; the official parser registry is
published alongside as `jsonize-registry.tar.gz` with a `.sha256` file.

## Two ways to feed jz

| Mode | Command | When to use |
|------|---------|-------------|
| **exec** | `jz run [flags] <command> [args...]` | jz starts the command itself. It knows the arguments and the operating system, forces `LC_ALL=C` for stable output, passes stderr through and mirrors the command's exit status. |
| **pipe** | `jz parse [flags] <command>` | You already have the text (a pipe, a log, a file via `--file`). Nothing is executed; the variant is chosen from the text unless you add `--os` or `--variant`. |

The mode is always explicit. jz never guesses from whether stdin is a
terminal, so scripts behave the same in CI and in a shell.

Everything after the command name in `jz run` belongs to the command,
including things that look like flags:

```console
$ jz run ps aux
$ jz run --variant gnu-human -- df -h
$ jz parse --os darwin --file captured-df.txt df
```

### Output flags

| Flag | Effect |
|------|--------|
| `-p`, `--pretty` | indent the JSON |
| `-r`, `--raw` | skip type conversion; every value stays a string |
| `--meta` | wrap the result: `{"command","variant","source",...,"data"}` |
| `--variant NAME` | pick a variant explicitly |
| `--os GOOS` | tell the selector which OS produced the output |
| `--registry DIR` | add a registry directory (repeatable, earliest wins) |
| `--embedded-only` | ignore user, cached and `JSONIZE_REGISTRY_PATH` registries |

`jz run` also accepts `--env NAME=value`, `--keep-locale`, `--timeout`
and `--max-output`; `jz parse` accepts `--file` and `--max-input`.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | unexpected failure (I/O, internal) |
| 2 | usage error |
| 3 | the input did not match the selected definition |
| 4 | no definition for the command, no variant matched, or several matched equally |
| 5 | a registry could not be loaded, validated or updated |
| *n* | `jz run` mirrors the command's own non-zero status (128+signal when it was killed); its output is still parsed when there is any |

Diagnostics always go to stderr with a `jz:` prefix. Stdout carries JSON
or nothing.

## Supported commands

`jz list` prints the current table. The embedded registry ships:

| Command | Variants |
|---------|----------|
| `df` | `gnu`, `gnu-human`, `bsd`, `bsd-human`, `busybox-human` |
| `free` | `gnu`, `gnu-human`, `gnu-wide` |
| `ps` | `unix` (`ps -ef`), `bsd` (`ps aux`), `busybox` |
| `uptime` | `linux`, `bsd` |
| `w` | `linux`, `bsd` |
| `mount` | `linux`, `bsd` |
| `uname` | `linux`, `darwin` |
| `ls` | `long`, `long-human` |
| `lsblk` | `linux` |
| `id`, `env`, `du`, `wc` | `posix` |

A *variant* is one output format of a command. GNU `df`, `df -h`, macOS
`df` and BusyBox `df -h` are four formats, so they are four definitions
under `df`. When one format is printed by several implementations (GNU and
BusyBox `df` without options are identical) a single definition covers
both and says so in `metadata.compatible`.

## How a variant is chosen

Each definition declares `detect` criteria:

```yaml
detect:
  os: [linux]                       # GOOS values
  args: {any: ["-h"], none: ["-i"]} # exec mode only; -hT counts as -h
  signature:
    all: ['^Filesystem\s+Size\s+Used\s+Avail\s+Use%\s+Mounted on\s*$']
  priority: 0
```

Every candidate for the command is scored by how many criteria were both
applicable and satisfied; a criterion that applies and fails rejects the
candidate. In pipe mode the OS and arguments are unknown, so only the
signature (matched against the first lines of the text, in multi-line
mode) decides unless you pass `--os`. The single most specific survivor
wins, `priority` breaks ties, and a remaining tie is an error that lists
the candidates, never a silent guess:

```console
$ cat weird.txt | jz parse df
jz: no variant of "df" matches the input
  - bsd: os unknown (pass --os to use it); signature all[0] /^Filesystem\s+512-blocks.../ did not match
  - gnu: os unknown (pass --os to use it); arguments unknown in pipe mode; signature all[0] ... did not match
  ...
use --variant to choose one explicitly
```

## Registries

Definitions come from layered registries; the first source that defines a
`command/variant` wins, so you can override an official parser without
editing it.

1. `--registry DIR` flags, in the order given
2. directories in `$JSONIZE_REGISTRY_PATH` (separated like `PATH`)
3. the user registry: `$XDG_CONFIG_HOME/jsonize/registry` on Linux,
   `~/Library/Application Support/jsonize/registry` on macOS,
   `%AppData%\jsonize\registry` on Windows
4. the cached official registry installed by `jz registry update`
5. the registry embedded in the `jz` binary

`jz registry paths` prints this list with what exists on your machine.
`jz list` shows which source each definition came from and `jz show`
reports shadowed sources.

A registry directory looks like this:

```
registry.yaml                                   # format: 1, name, version
parsers/<command>/<variant>/parser.yaml         # the definition
parsers/<command>/<variant>/testdata/<case>.txt # captured output
parsers/<command>/<variant>/testdata/<case>.json# expected JSON
parsers/<command>/<variant>/testdata/<case>.yaml# optional: os, args, source, expect_error
```

### Updating the official registry without upgrading jz

```console
$ jz registry update
installed registry in ~/.cache/jsonize/registry/official (46 files, 61234 bytes, sha256 3f9c1a2b7e4d)
```

The archive is fetched over HTTPS from the jsonize release page, checked
against its SHA-256 file, extracted with path-traversal and size guards,
validated (every definition must load) and only then swapped into place
with a rename. A failed update leaves the previous registry untouched.
`--source URL` points at another archive; `--dir` installs elsewhere.

## Adding a parser (no Go required)

1. Create the directory and definition:

   ```console
   $ mkdir -p ~/.config/jsonize/registry/parsers/greet/default/testdata
   $ cat > ~/.config/jsonize/registry/parsers/greet/default/parser.yaml <<'YAML'
   format: 1
   command: greet
   variant: default
   description: greeting lines
   parse:
     type: regex
     pattern: '^hello (?P<name>\S+) x(?P<times>\d+)$'
   fields:
     times: {type: int}
   YAML
   ```

2. Add captured output as `testdata/basic.txt`.
3. Generate and review the golden file, then validate:

   ```console
   $ jz validate --update ~/.config/jsonize/registry
   WROTE .../testdata/basic.json
   $ jz validate ~/.config/jsonize/registry
   ok   greet/default [basic]
   ```

4. Use it: `greet | jz parse greet` or `jz run greet`.

The full definition language (tables split by whitespace, header
alignment or a delimiter; regular expressions; key/value lines; composite
parsers; typed fields including sizes with units, arrays and nested
objects) is documented in [docs/definition-format.md](docs/definition-format.md),
and [docs/adding-parsers.md](docs/adding-parsers.md) walks through
contributing one to the official registry.

## Security model in one paragraph

A definition is data. It can select lines, split them and convert values;
it cannot run programs, read files or evaluate expressions. Regular
expressions use Go's RE2 engine (linear time, no catastrophic
backtracking) and are bounded in length. Inputs are bounded in total size
and line length. Registry archives are fetched only over HTTPS, checksum
verified, extracted without honouring absolute paths, `..`, symlinks or
special files, and installed atomically. See [SECURITY.md](SECURITY.md).

## Development

```console
$ make help            # every target
$ make build           # dist/jz
$ make test            # unit + golden tests with coverage (cover.out)
$ make test-race
$ make lint            # golangci-lint for linux, darwin and windows
$ make fuzz            # every Fuzz* target for 10s (FUZZTIME=…)
$ make e2e             # atago end-to-end suite (go install github.com/nao1215/atago@latest)
$ make bench           # benchmarks into bench/new.txt; make bench-compare against the baseline
$ make golden-update   # regenerate registry/**/testdata/*.json after changing a definition
```

The design, its trade-offs and what was deliberately left out are written
up in [docs/design.md](docs/design.md).

## License

[MIT](LICENSE)
