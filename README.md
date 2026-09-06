# jsonize

**jsonize — Turn command output into JSON.**

Pipe a command's output to `jz` and get JSON. jz works out which command
produced the text and how to read it, so there is nothing to configure
and nothing to name:

```console
$ df -h | jz
[{"filesystem":"/dev/nvme0n1p2","size":"1.8T","used":"1.6T","available":"145G","use_percent":92,"mounted_on":"/"}, ...]

$ ps aux | jz | jq '.[] | select(.cpu_percent > 10) | .command'
"/usr/lib/firefox/firefox"

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

Parsers are YAML definitions in a registry, not Go code: supporting a new
command, another system's variant of it, or a new option is a pull
request containing only YAML and fixtures.

## Install

```console
# from source (Go 1.26 or later)
go install github.com/nao1215/jsonize/cmd/jz@latest

# from a release archive
curl -sSfL https://github.com/nao1215/jsonize/releases/latest/download/jsonize_<version>_linux_amd64.tar.gz | tar xz
```

## Using it

```console
COMMAND | jz                 # convert piped output
jz < captured.txt            # or output captured earlier
jz --file captured.txt
jz run COMMAND [args...]     # let jz run the command and convert its stdout
```

`jz run` is the helper for the case where jz starts the command itself. It
hands the command your standard input, passes its standard error through
untouched, mirrors its exit status, and runs it with `LC_ALL=C` so the
output is the one the parsers describe.

Everything after the command name belongs to the command, including
things that look like jz options. A bare `--` states that boundary:

```console
$ jz run ps aux
$ jz run mytool --pretty            # --pretty goes to mytool
$ jz run --pretty -- mytool --json  # --pretty is jz's, --json is mytool's
```

### Options

```text
  -f, --file PATH     read input from PATH instead of stdin
  -p, --pretty        indent JSON output
      --parser NAME   restrict detection to one parser
      --variant NAME  use a variant of --parser
  -h, --help          show help
```

`--parser` and `--variant` are for the cases detection cannot settle on
its own: a format too generic to claim, a wrapper whose name is not the
tool it runs, a variant you want pinned in CI. `--variant` needs
`--parser`, because a variant name only identifies a definition together
with its parser:

```console
$ git diff --numstat | jz --parser du     # deliberately reading it as du
$ df -h | jz --parser df --variant gnu-human
$ jz run --parser dir -- cmd.exe /c dir
```

Naming a parser is a claim about the input, not a way around the checks:
the definition's signature still has to fit the text, and jz fails if it
does not.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | unexpected failure (I/O, internal) |
| 2 | usage error |
| 3 | the input did not match the chosen definition |
| 4 | the format could not be identified, several matched, or a named one did not fit |
| 5 | a registry could not be loaded |
| *n* | `jz run` mirrors the command's own non-zero status (128+signal when it was killed); its output is still parsed when there is any |

Diagnostics always go to stderr with a `jz:` prefix. Stdout carries
complete JSON or nothing at all: a failure never leaves a half-written
document behind.

## What jz can read

`jz list` prints the current table, `jz list df` the variants of one
command, and `jz list df gnu` everything about one definition.

| Command | Variants |
|---------|----------|
| `df` | `gnu`, `gnu-human`, `bsd`, `bsd-human`, `busybox-human` |
| `free` | `gnu`, `gnu-human`, `gnu-wide` |
| `ps` | `unix` (`ps -ef`), `bsd` (`ps aux`), `busybox` |
| `uptime` | `linux`, `bsd` |
| `w` | `linux`, `bsd` |
| `mount` | `linux`, `bsd` |
| `uname` | `linux`, `darwin` |
| `ls` | `long` (`ls -l` and `ls -lh`) |
| `lsblk` | `linux` |
| `env` | `posix`, `null-separated` (`env -0`) |
| `id`, `du`, `wc` | `posix` |

A *variant* is one output format of a command. GNU `df`, `df -h`, macOS
`df` and BusyBox `df -h` are four formats, so they are four definitions.
When one format is printed by several implementations, a single
definition covers them all and says so in `metadata.compatible`.

## How jz decides

Every definition carries a signature: the shape its output must have.

```yaml
detect:
  os: [linux]                       # the systems this format comes from
  args: {any: ["-h"], none: ["-i"]} # only checked when jz ran the command
  signature:
    all: ['^Filesystem\s+Size\s+Used\s+Avail\s+Use%\s+Mounted on\s*$']
```

A signature is a necessary condition: if it does not match, the
definition is out. The operating system and the arguments are known only
when jz ran the command itself, and they can then remove candidates but
never promote one. Exactly one survivor is a success. Zero and more than
one are both errors that say what to pass:

```console
$ cat weird.txt | jz
jz: unable to identify the input format
no signature of the 26 known parsers matched this text

Name the parser explicitly:
  COMMAND | jz --parser df
Run `jz list` to see the supported parsers.
```

Some formats are too unremarkable to claim. A number, a tab and a path is
`du` output and `git diff --numstat` alike, so `du` is only used when you
name it:

```console
$ git diff --numstat | jz
jz: unable to identify the input format
it could be `du` output, but that format is too generic for jz to claim on its own

Confirm it:
  COMMAND | jz --parser du
```

### What jz will not invent

`df -h` counts 1024 per suffix step and `df -H` counts 1000, the output
says which nowhere, and both round. jz therefore reports a
human-readable size exactly as it was printed (`"1.8T"`) instead of
deriving a byte count it cannot actually know. Ask the command for exact
numbers when you need them: `df` (1K blocks), `free` (kibibytes),
`ls -l` (bytes), `lsblk -b` (bytes).

Similarly, `env` output cannot express a value containing a newline;
`env -0 | jz` can, and jz has a parser for it.

## Registries

Definitions come from layered registries, and the first one that defines
a `command/variant` wins, so a local definition can override an official
one without editing it:

1. directories in `$JSONIZE_REGISTRY_PATH` (separated like `PATH`)
2. the user registry: `$XDG_CONFIG_HOME/jsonize/registry` on Linux,
   `~/Library/Application Support/jsonize/registry` on macOS,
   `%AppData%\jsonize\registry` on Windows
3. the registry embedded in the `jz` binary

`jz list --sources` prints this list with what exists on your machine and
how many definitions came from each. jz never accesses the network: the
same input converts to the same JSON on the same machine, always.

A registry directory looks like this:

```
registry.yaml                                    # format: 1, name, version
parsers/<command>/<variant>/parser.yaml          # the definition
parsers/<command>/<variant>/testdata/<case>.txt  # captured output
parsers/<command>/<variant>/testdata/<case>.json # expected JSON
parsers/<command>/<variant>/testdata/<case>.yaml # optional: os, args, source
```

## Adding a parser (no Go required)

1. Create the directory and the definition:

   ```console
   $ mkdir -p ~/.config/jsonize/registry/parsers/greet/default/testdata
   $ cat > ~/.config/jsonize/registry/parsers/greet/default/parser.yaml <<'YAML'
   format: 1
   command: greet
   variant: default
   description: greeting lines
   detect:
     signature:
       all: ['^hello \S+ x\d+$']
   parse:
     type: regex
     pattern: '^hello (?P<name>\S+) x(?P<times>\d+)$'
   fields:
     times: {type: int}
   YAML
   ```

2. Save captured output as `testdata/basic.txt`.
3. Generate the expected JSON, read it, then check it stays that way:

   ```console
   $ make registry-update-golden DIR=~/.config/jsonize/registry
   $ make registry-test DIR=~/.config/jsonize/registry
   ```

4. Use it: `greet | jz`.

The definition language is documented in
[docs/definition-format.md](docs/definition-format.md), and
[docs/adding-parsers.md](docs/adding-parsers.md) walks through
contributing one to the official registry.

## Security model in one paragraph

A definition is data. It can select lines, split them and convert values;
it cannot run programs, read files or make network requests. Regular
expressions use Go's RE2 engine (linear time, no catastrophic
backtracking) and are bounded in length. Input is bounded at 64 MiB and
lines at 1 MiB. `jz run` executes exactly the command you named, with no
shell involved, and only after jz knows a parser exists for it. See
[SECURITY.md](SECURITY.md).

## Development

```console
$ make help                    # every target
$ make build                   # dist/jz
$ make test                    # unit and golden tests with coverage
$ make test-race
$ make lint                    # golangci-lint for linux, darwin and windows
$ make fuzz                    # every Fuzz target briefly (FUZZTIME=10s)
$ make e2e                     # atago end-to-end suite
$ make bench                   # benchmarks; make bench-compare against the baseline
$ make registry-test           # validate definitions and run their golden cases
$ make registry-update-golden  # rewrite the expected JSON, then read the diff
```

The design and its trade-offs are written up in
[docs/design.md](docs/design.md).

## License

[MIT](LICENSE)
