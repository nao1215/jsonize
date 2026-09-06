# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

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
- Official definitions for `df`, `free`, `ps`, `uptime`, `w`, `mount`,
  `uname`, `ls`, `lsblk`, `id`, `env`, `du` and `wc`.

### Decisions worth knowing

- A rounded, human-readable size (`df -h`, `free -h`, `ls -lh`, `lsblk`)
  is reported exactly as printed. The base is not recoverable from the
  output and the value is rounded, so a byte count would be invented.
- A format too generic to recognise (`du`, `wc`) is only used when the
  parser is named; its signature is still checked then.
- Naming a parser or variant never disables the signature check.
- The public options are `--file`, `--pretty`, `--parser`, `--variant`
  and `--help`. The input limit is 64 MiB and is not configurable.
