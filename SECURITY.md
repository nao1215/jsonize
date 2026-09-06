# Security policy

## Trust model

- **Parser definitions are data.** A definition can select lines, split
  them, match regular expressions and convert values. It cannot run
  commands, read files, make network requests or evaluate expressions.
  Regular expressions are compiled by Go's `regexp` (RE2) which guarantees
  linear-time matching; their length is capped. Nesting depth, column
  counts and part counts are bounded by the validator.
- **Input is bounded.** `jz parse` refuses input above `--max-input`
  (64 MiB by default) and lines above 1 MiB; `jz run` stops a command
  whose stdout exceeds `--max-output`.
- **`jz run` executes exactly the command you named.** No shell is
  involved, arguments are passed verbatim, and the command is looked up on
  `PATH` like any other program. jz will not execute a command it has no
  definition for.
- **Registry updates are fetched over HTTPS only.** The archive must match
  the SHA-256 published next to it, is extracted without honouring
  absolute paths, `..`, symlinks, hard links or device nodes, is bounded
  in file count and total size, must load cleanly, and is then installed
  with an atomic rename. A failed update leaves the previous registry in
  place. Plain `http://` is rejected unless `--allow-http` is given, which
  exists for tests.
- **Layered registries.** A definition in a user or `--registry` directory
  shadows the official one of the same name. Only put directories you
  control there; `jz list` shows where each definition came from.

What the checksum does *not* give you: authenticity independent of the
download host. The checksum file lives beside the archive on the release
page, so it detects corruption and truncation, not a compromised host.
Release archives of the `jz` binary carry GitHub build provenance
attestations; signing the registry archive with a key held outside the
CI is planned but not part of the current release process.

## Supported versions

The latest minor release receives fixes. Older releases are not patched.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability
reporting on this repository, or email `n.chika156@gmail.com` with a
description, the affected version and a reproduction. You will get an
acknowledgement within a few days and a fix or mitigation plan as soon as
one is available. Credit is given in the release notes unless you prefer
otherwise.
