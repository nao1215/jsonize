# Security policy

## Trust model

- Parser definitions are data. A definition can select lines, split
  them, match regular expressions and convert values. It cannot run
  commands, read files, make network requests or evaluate expressions.
  Regular expressions are compiled by Go's `regexp` (RE2) which guarantees
  linear-time matching; their length is capped. Nesting depth, column
  counts and part counts are bounded by the validator.
- Input is bounded. Whole-document mode refuses input above 64 MiB.
  Streaming bounds the input retained for an unfinished record at 64 MiB,
  rather than limiting the total stream. Both modes limit lines to 1 MiB.
  These limits are internal.
- `jz run` executes exactly the command you named. No shell is
  involved, arguments are passed verbatim, and the command is looked up on
  `PATH` like any other program. It needs a registered parser for that
  name, an explicit `--parser`, or an inline `--define`. The command
  itself runs with your permissions and may access files or the network.
- No registry downloads. jz never fetches definitions: the official registry
  is embedded in the binary and additional definitions come from local
  directories only.
- Layered registries. A definition in the user registry or in a
  directory named by `JSONIZE_REGISTRY_PATH` shadows the official one of
  the same name. Only point those at directories you control;
  `jz list --sources` shows where definitions came from and
  `jz list COMMAND VARIANT` shows which file a definition lives in.
- A named parser is still checked. `--parser` and `--variant` narrow
  the candidates; they do not disable the signature check, and there is
  no option that does.

Release binaries carry GitHub build provenance attestations.

## Supported versions

The latest minor release receives fixes. Older releases are not patched.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability
reporting on this repository, or email `n.chika156@gmail.com` with a
description, the affected version and a reproduction. You will get an
acknowledgement within a few days and a fix or mitigation plan as soon as
one is available. Credit is given in the release notes unless you prefer
otherwise.
