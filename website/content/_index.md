jsonize turns command output into JSON. Pipe a command to `jz` to detect
its format and convert it. It also supports YAML output and streaming.

```console
$ df -h | jz
[{"filesystem":"/dev/nvme0n1p2","size":"1.8T","used":"1.6T","available":"145G","use_percent":92,"mounted_on":"/"}, ...]

$ ps aux | jz | jq '.[] | select(.cpu_percent > 10) | .command'
"/usr/lib/firefox/firefox"
```

![jz reading df, uptime and free, and refusing input it cannot identify](demo/jsonize.gif)

Parsers are YAML definitions, and jz is one binary with no dependencies
outside the Go standard library.

## Install

With Go 1.26 or later:

```sh
go install github.com/nao1215/jsonize/cmd/jz@latest
```

See the [install guide](install/) for release archives and shell completion.

## Detection and limits

Some formats need a parser name because their text is too generic to
identify automatically:

```console
$ git diff --numstat | jz
jz: unable to identify the input format
it could be `git` output, but that format is too generic for jz to claim on its own

Confirm it:
  COMMAND | jz --parser git
```

A rounded size such as `"1.8T"` stays a string. Use `df`, `free` or
`lsblk -b` for exact numbers.

Parsers are YAML definitions. Add a definition and captured output to a
local registry to support another format without rebuilding jz.

## Where to go next

- [Install](install/) the binary.
- [Usage](usage/) covers the two ways in and the exit codes.
- [Parsers](parsers/) lists what jz reads today.
- [Coverage](coverage/) lists supported options and known limitations.
- [Write a parser](write-a-parser/) is the guide for adding one.
- [Definition format](definition-format/) is the reference for the YAML.
- [Design](design/) records why the tool is shaped this way.
