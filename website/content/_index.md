jsonize reads the output a command already printed and writes JSON. It
works out which command produced the text, so there is nothing to name
and nothing to configure:

```console
$ df -h | jz
[{"filesystem":"/dev/nvme0n1p2","size":"1.8T","used":"1.6T","available":"145G","use_percent":92,"mounted_on":"/"}, ...]

$ ps aux | jz | jq '.[] | select(.cpu_percent > 10) | .command'
"/usr/lib/firefox/firefox"
```

![jz converting df, ps and uptime output](demo/jsonize.gif)

Parsers are YAML definitions in a registry rather than Go code, so
supporting another command, another system's variant of it, or another
option is a file and a fixture, not a release.

## Why the guessing stops where it does

Converting output nobody asked about is easy; converting it wrongly and
saying nothing is the failure that matters. jz refuses instead:

```console
$ git diff --numstat | jz
jz: unable to identify the input format
it could be `du` output, but that format is too generic for jz to claim on its own

Confirm it:
  COMMAND | jz --parser du
```

A rounded size is reported the way it was printed rather than turned into
a byte count. `df -h` counts 1024 per suffix step and `df -H` counts
1000, the output records neither, and both round, so `"1.8T"` is what jz
knows and all it claims. Run the exact form of a command when you need
numbers.

## Where to go next

- [Install](install/) the binary.
- [Usage](usage/) covers the two ways in and the exit codes.
- [Parsers](parsers/) lists what jz reads today.
- [Write a parser](write-a-parser/) is the guide for adding one.
- [Definition format](definition-format/) is the reference for the YAML.
- [Design](design/) records why the tool is shaped this way.
