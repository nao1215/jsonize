# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `jz run` (exec mode) and `jz parse` (pipe mode) with an explicit
  exit-code contract and stderr passthrough.
- YAML parser definitions (format 1) with table, regex, key/value and
  composite parsers and typed field conversion.
- Variant selection from OS, arguments and output signatures with
  explicit ambiguity errors.
- Layered registries: `--registry`, `JSONIZE_REGISTRY_PATH`, user
  directory, cached official registry, embedded registry.
- `jz registry update` with HTTPS, SHA-256 verification, safe extraction
  and atomic install.
- `jz validate --update` so parsers can be authored without Go.
- Official definitions for `df`, `free`, `ps`, `uptime`, `w`, `mount`,
  `uname`, `ls`, `lsblk`, `id`, `env`, `du` and `wc`.
