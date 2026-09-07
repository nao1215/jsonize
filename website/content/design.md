---
title: Design
description: The decisions behind jsonize, the alternatives considered and what was left out.
toc: true
---

This document records the decisions behind jsonize, the alternatives that
were considered and what the MVP deliberately leaves out.

## Goals

- Turn command output into JSON without writing Go per command.
- Make the chosen parser and the reason for choosing it visible; never
  produce plausible-looking JSON from the wrong parser.
- Keep definitions safe to accept from third parties.
- Behave identically in a terminal, a pipeline and CI.

## Architecture

```
cmd/jz                    entry point: signals, exit code
internal/cli              subcommands, flag parsing, registry layering, exit-code contract
internal/runner           exec mode: child process, LC_ALL=C, stderr passthrough, output cap, signals
internal/registry         load registry directories/FS, merge with precedence, testdata cases
internal/definition       YAML schema, validation, regex compilation, format/version checks
internal/selector         variant selection (os / args / signature filter → priority → error)
internal/engine           parse algorithms (table, regex, kv, composite) and field conversion
internal/convert          scalar conversions (int, float, bool, size with units)
internal/jsonutil         insertion-ordered JSON object and encoder
internal/conformance      golden cases and the definition/fixture cross product, shared by `go test` and `jz test`
internal/buildinfo        the version string stamped at build time
registry/                 the official definitions and fixtures (data) + a one-file embed
e2e/atago                 end-to-end scenarios
```

Data flows in one direction: capture → select → parse → encode. The
selector needs only the first lines of the text; the engine needs the
compiled definition and the whole text; encoding never sees definitions.

## Decisions and trade-offs

### A small public surface

The command line is four subcommands (`run`, `list`, `test`, `version`)
and five options (`--file`, `--pretty`, `--parser`, `--variant`, `--help`). Every
option that asked the user to make a decision jz should be making, or
that changed the output contract, was removed before release:

| Removed | Why |
|---------|-----|
| `--raw` | a second output shape for the same input; the typed one is the contract |
| `--meta` | an envelope that changed the JSON schema depending on a flag |
| `--os` | asking the user to declare the producing system instead of matching the text; jz knows the system when it runs the command itself |
| `--force` | a way to parse with a definition whose signature says the text is something else, which is exactly the failure jz exists to prevent |
| `--max-input` | a safety limit, not a preference: 64 MiB, internal |
| `--embedded-only`, `--registry` | registry plumbing; `JSONIZE_REGISTRY_PATH` covers the real use and tests inject sources directly |

### Definitions in YAML rather than Go plugins or a scripting language

Per-command code is the obvious design and it is where most output
converters end up: hundreds of hand-written parsers whose variant
detection is ad-hoc string sniffing and whose schema lives in comments.
jsonize instead fixes a small declarative language. The price is
expressiveness: a format whose shape comes from the input rather than
from the definition, such as a tree of arbitrary depth, cannot be
expressed. The gain is that every definition is reviewable, testable by
a fixture, and safe to load from a third party. YAML was chosen over JSON (no
comments) and TOML (awkward nesting); `goccy/go-yaml` provides strict
decoding and line numbers in errors.

### A handful of parse types, not a general pipeline

`table`, `regex`, `kv`, `composite` and `records` cover the shapes the
registered commands print. A composable step pipeline (split → map →
filter …) was considered and rejected: it is harder to validate, harder
to explain, and every real example so far fits one of the five. The
`input.select` block (after/until/skip/limit) plus `composite` gives
section handling without a pipeline, `records` applies one description
to a block that repeats, and a `composite` part may be `records` so that
a report with a header block above the repeats has a shape too. Adding a
type does not change the format version, because an unknown type is
already an error.

### Table splitting

- `whitespace`: split on runs of whitespace with the last column
  absorbing the remainder. Used for `df`, `ps`, `free`.
- `aligned`: cut cells at the rune offsets where header words start,
  moving a boundary left when a right-aligned value is wider than its
  header and keeping a token whole when it overflows to the right. Empty
  cells become `null`. Needed for `lsblk` and `w`.
- `delimiter`: a literal separator (`du`'s tab).

Explicit `header.columns` is preferred in the official registry because
derived names depend on the exact header text; derivation exists for
quick local definitions.

### Variant = output format

One definition per *format*, not per implementation. GNU and BusyBox `df`
without options print the same table, so one definition covers both and
lists them in `metadata.compatible`; `df -h` differs between them ("Avail"
vs "Available") and gets two definitions. This keeps definitions
honest: a definition claims exactly what its fixtures prove.

### Rounded numbers stay text

`df -h` steps by 1024 and `df -H` by 1000; the output records neither,
and both round. Deriving bytes from "1.1G" would invent both a base and a
precision, so human-readable sizes are reported exactly as printed and
the exact forms of the same commands (`df`, `free`, `ls -l`, `lsblk -b`)
are what produce numbers. The same reasoning removed the `ls -l` /
`ls -lh` split: which one produced a listing is not decidable from the
text, and with the size kept as printed it does not need to be.

### Some formats are only used when named

A signature is a necessary condition, but a weak one can still be met by
unrelated text: a number, a tab and a path is `du` output and
`git diff --numstat` alike. Such a definition sets
`detect.auto_detect: false`, which keeps it out of automatic detection
while `--parser du` and `jz run du` still reach it, signature check
included. The alternative, adding exclusion patterns for every other
format that happens to look similar, is a list that can never be
finished.

### Selection never guesses

Candidates are filtered by criteria that apply (a criterion whose input is
unknown, such as arguments in pipe mode, neither helps nor hurts). One
survivor is the answer; there is no ranking by how closely a definition
fits. Several survivors are settled by `detect.priority` only when they
are variants of the same command and one priority is strictly highest,
which is a statement the definition author made deliberately; anything
else is an error that names the candidates. The alternative, "first match
wins", would silently depend on directory order.

Survivors from different registries are settled before that, by the
layering: the definition from the earlier registry wins. This is a
declaration rather than a guess, and it is the same declaration that
already decides which definition of one command and variant applies. The
alternative was to keep it an error, which meant that one definition
someone added locally could take an official parser away from them: with
a `^Filesystem` signature in a user registry, `df -h | jz` reported that
the input matched several parsers and exited 4. Definitions inside one
registry are still never ranked against each other, so a collision the
registry owns stays an error they have to resolve. `jz test` is where
they see it.

Fixtures double as selection tests: every fixture is pushed through the
selector and must pick its own definition, and every definition is named
explicitly on every other definition's fixtures and must refuse them, so
adding a variant whose signature overlaps an existing one fails
`jz test` immediately.

### Exec mode forces `LC_ALL=C`

Output formats are documented for the C locale; translated headers and
localized numbers break parsers. jz sets `LC_ALL=C` and `LANG=C` (and
drops `LANGUAGE`) unless `--keep-locale` is given. Pipe mode cannot
control the producer, which is why signatures match structure rather than
prose where possible.

### Exit status of `jz run`

A failing command's status is mirrored, and its output is still parsed
when there is any (for example `df` exits 1 when a mount point is
unreadable but prints the table). jz's own codes (2–5) are documented and
distinct from 0/1 so scripts can tell them apart, but they can collide
with a child's codes; the stderr line `jz: <cmd> exited with status N`
disambiguates.

### Registry layering and the code/data boundary

`registry/` holds only YAML, fixtures and one `embed.go`. `internal/*`
never imports it; only `cmd/jz` and the golden test do. Moving the
registry to its own repository means changing one import.

Layering (`JSONIZE_REGISTRY_PATH` → user registry → embedded) lets a user
fix a parser locally today and ship it upstream tomorrow with no change
in behaviour.

### No network, ever

jz reads local directories and nothing else. A release carries both the
code and the definitions, so a given input converts to the same JSON on a
given machine whatever the network is doing. Updating the official
registry means installing a new jz; adding your own means pointing
`JSONIZE_REGISTRY_PATH` at a directory.

### Format versioning

`format: 1` is the schema major version. A different number is rejected
with a message that says whether to upgrade jz or the definition.
`min_jsonize` lets a definition require a newer jz for a feature added
without a format bump. Development builds skip the check.

### Dependencies

- `github.com/goccy/go-yaml` — strict YAML decoding with positions.
- `github.com/google/go-cmp` — structural diffs in golden failures.

No CLI framework: four subcommands with a handful of flags each are
served by `flag` and a dispatch table, and the `run` subcommand needs
"stop at the first non-flag" semantics that `flag` gives for free.

## Deliberately out of scope for the MVP

- Streaming parsers (line-at-a-time output for long-running commands).
- Recursive sections, where the depth comes from the input (`ls -R`,
  `npm ls --all`, `docker info`). A block that repeats at one level is
  `records`, and a value continued on the next line is `input.fold`; a
  tree is neither.
- Derived fields (computing `uptime_seconds` from `"13 days, 4:30"`).
- Two commands that print the same format under both their names.
  Nothing in the text says which of them wrote it, so `vdir`, `getent`
  and `printenv` are left to `ls`, `etc` and `env`.
- Fetching or updating registries over the network.
- A JSON Schema for editor completion of `parser.yaml`; validation is
  done in Go with path-qualified messages instead.

## Where to cut next

- `registry/` → separate repository; only the `official` import changes.
- `internal/definition` + `internal/engine` + `internal/convert` form a
  library with no CLI dependencies and could be exported as a package.
- `internal/selector` and `internal/engine` are independent of the CLI and
  could be exercised by other front ends.
