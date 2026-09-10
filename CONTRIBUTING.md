# Contributing to jsonize

Thank you for helping. Most contributions are parser definitions, which
need no Go at all; the sections below cover both kinds.

## Contributing a parser definition

1. Capture real output with `LC_ALL=C` from the implementation you are
   describing (GNU coreutils, BusyBox, macOS, ...). Do not invent output.
   Trim it to a handful of representative lines and replace personal
   identifiers (user and host names) consistently.
2. Create `registry/parsers/<command>/<variant>/parser.yaml`. Variant
   names describe the *format* (`gnu`, `gnu-human`, `bsd`, `busybox`),
   not the machine it came from. Read
   the [definition format](https://nao1215.github.io/jsonize/definition-format/)
   and the [guide to writing one](https://nao1215.github.io/jsonize/write-a-parser/).
3. Put the capture in `testdata/<case>.txt` and describe it in
   `testdata/<case>.yaml` (`source`, and `os`/`args` so the fixture is
   also used to test variant selection).
4. Generate the golden file and check it by eye:

   ```console
   $ make registry-update-golden
   $ git diff registry/
   ```

5. Publish the schema of what the definition produces, and check it by
   eye:

   ```console
   $ make registry-update-schema
   $ git diff registry/schemas
   ```

   A change to an existing definition that could break a program reading
   its output (a key removed or retyped, a key no longer always there) is
   refused until it is meant: `make registry-update-schema
   BREAKING=command/variant` publishes it under the next contract version.
   Say why in `CHANGELOG.md`.

6. Run `make test`. The golden test proves that every fixture parses,
   selects its own variant unambiguously, reads all of its input, fits its
   schema and matches its JSON.

Every definition needs a `detect.signature`: it is what lets jz accept the
text as that format and reject anything else. When the shape is too
generic to be evidence on its own (a number and a path, three numbers),
add `detect.auto_detect: false` so the definition is only used when the
user names the parser.

Do not convert a rounded, human-readable number to bytes. The output does
not record the base, and the value is rounded; keep it as printed and let
the exact form of the command produce numbers.

## Contributing code

1. Discuss larger changes in an issue first.
2. Keep the definition language small. A new key must be something a
   reviewer can reason about from the YAML alone; nothing may execute
   code or touch the filesystem.
3. Run the same checks as CI before opening a pull request:

   ```console
   $ make check      # fmt, vet, lint (three GOOS), test, race
   $ make e2e        # needs atago: go install github.com/nao1215/atago@latest; see e2e/README.md
   $ make fuzz FUZZTIME=5s
   ```

4. Add or extend tests next to the code. Golden tests for the registry,
   unit tests for engine/selector/convert behaviour, atago scenarios for
   anything visible on the command line (exit codes, stderr, JSON shape).
   A scenario that needs a command the CI image lacks is installed by
   `.github/workflows/e2e.yml`, not gated on the command being there;
   only a scenario that needs a device may skip, in `hardware_linux`.
5. If a change affects performance, run `make bench` and
   `make bench-compare`; commit an updated `bench/baseline.txt` only when
   the change is intentional.
6. Write commit messages in the Conventional Commits style
   (`feat:`, `fix:`, `parser:`, `docs:`, `test:`, `chore:`).

## Definition format changes

`format` versions how a definition is written, not what it produces; the
output contract has a version of its own in `registry/schemas`.
Changing the meaning of an existing key or adding a required one is a
breaking change to `format`. Bump `definition.CurrentFormat`, document
the migration in `website/content/definition-format.md`, and update every
embedded definition. Additive, optional keys keep the format number.

## Releasing

Tag `vX.Y.Z` on `main`. GoReleaser builds the binaries for Linux, macOS
and Windows and publishes them to the GitHub release with build
provenance attested. The registry ships inside the binary, so a release
is the unit that carries both the code and the definitions.
