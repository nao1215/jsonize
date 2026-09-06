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
   [docs/definition-format.md](docs/definition-format.md) and
   [docs/adding-parsers.md](docs/adding-parsers.md).
3. Put the capture in `testdata/<case>.txt` and describe it in
   `testdata/<case>.yaml` (`source`, and `os`/`args` so the fixture is
   also used to test variant selection).
4. Generate the golden file and check it by eye:

   ```console
   $ make golden-update
   $ git diff registry/
   ```

5. Run `make test`. The golden test proves that every fixture parses,
   selects its own variant unambiguously and matches its JSON.

Definitions must contain a `detect.signature` whenever a command has more
than one variant, so that piped input can be classified.

## Contributing code

1. Discuss larger changes in an issue first.
2. Keep the definition language small. A new key must be something a
   reviewer can reason about from the YAML alone; nothing may execute
   code or touch the filesystem.
3. Run the same checks as CI before opening a pull request:

   ```console
   $ make check      # fmt, vet, lint (three GOOS), test, race
   $ make e2e        # needs atago: go install github.com/nao1215/atago@latest
   $ make fuzz FUZZTIME=5s
   ```

4. Add or extend tests next to the code. Golden tests for the registry,
   unit tests for engine/selector/convert behaviour, atago scenarios for
   anything visible on the command line (exit codes, stderr, JSON shape).
5. If a change affects performance, run `make bench` and
   `make bench-compare`; commit an updated `bench/baseline.txt` only when
   the change is intentional.
6. Write commit messages in the Conventional Commits style
   (`feat:`, `fix:`, `parser:`, `docs:`, `test:`, `chore:`).

## Definition format changes

Changing the meaning of an existing key or adding a required one is a
breaking change to `format`. Bump `definition.CurrentFormat`, document
the migration in `docs/definition-format.md`, and update every embedded
definition. Additive, optional keys keep the format number.

## Releasing

Tag `vX.Y.Z` on `main`. GoReleaser builds the binaries for Linux, macOS
and Windows, packages the registry as `jsonize-registry.tar.gz` with a
`.sha256` file and publishes everything to the GitHub release; build
provenance is attested. `jz registry update` picks the archive up from
the "latest" release.
