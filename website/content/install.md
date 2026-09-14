---
title: Install
description: Install jz from source or from a release archive.
---

## From source

Go 1.26 or later:

```sh
go install github.com/nao1215/jsonize/cmd/jz@latest
```

The executable is named `jz`. Add `$(go env GOPATH)/bin` to `PATH`, or
the directory named by `GOBIN` if you set it.

## From a package manager

On Arch Linux, the [`jsonize-bin`](https://aur.archlinux.org/packages/jsonize-bin)
package in the AUR installs the release binary:

```sh
yay -S jsonize-bin      # or: paru -S jsonize-bin
```

Releases after v0.1.0 publish a Homebrew cask in
[nao1215/homebrew-tap](https://github.com/nao1215/homebrew-tap), for
macOS and Linux:

```sh
brew install --cask nao1215/tap/jsonize
```

## From a release archive

[GitHub Releases](https://github.com/nao1215/jsonize/releases) has
archives for Linux, macOS and Windows on `amd64` and `arm64`. Linux and
macOS use `.tar.gz`; Windows uses `.zip`. Go is not required. FreeBSD
has no archive; install it from source.

Extract the archive and put `jz` (Windows: `jz.exe`) in a directory on
`PATH`. Run `jz version` to check it. Releases also include `.deb`,
`.rpm` and `.apk` packages for Linux, SHA-256 checksums in
`checksums.txt`, and GitHub build provenance for the archives and
packages:

```sh
sha256sum --ignore-missing -c checksums.txt            # Linux
shasum -a 256 --ignore-missing -c checksums.txt        # macOS
gh attestation verify jsonize_<version>_linux_amd64.tar.gz --repo nao1215/jsonize
```

Releases after v0.1.0 also sign `checksums.txt` with
[cosign](https://github.com/sigstore/cosign), without a key: the
signature in `checksums.txt.sigstore.json` names the release workflow
of this repository as the signer. Verify it before the checksums:

```sh
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/nao1215/jsonize/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

Each archive has an SPDX software bill of materials beside it,
`<archive>.sbom.json`, listing the Go modules and the Go standard library
built into `jz`.

The parser registry ships inside the binary, so a release carries both
the code and the definitions and jz never fetches anything at run time.

The Windows archive is a Windows binary: it runs, reads a file or a pipe
and writes JSON there like anywhere else. What it reads is a separate
question, answered on the [coverage page](../coverage/): `ipconfig`,
`ipconfig /all` and `systeminfo` are read, checked against the real
commands on English Windows Server 2022 and 2025, and no other Windows
command has a definition of its own.

## Check that it works

```sh
jz version
jz list
```

On Linux or macOS, try `df -h | jz`. On an English Windows system,
try `ipconfig | jz`.

## Shell completion

`jz completion bash` and `jz completion zsh` print a completion script.
It completes the subcommands, the options, the parsers of every registry
jz reads (the built-in one, your own and the directories in
`JSONIZE_REGISTRY_PATH`), the variants of the parser named on the line,
and file paths where a path goes. Completing reads those registries and
nothing else: it runs no command and opens no connection.

bash, in `~/.bashrc`:

```console
eval "$(jz completion bash)"
```

`eval` rather than `source <(jz completion bash)`, which the bash 3.2
macOS ships reads as nothing.

zsh, in `~/.zshrc` after `compinit`:

```console
source <(jz completion zsh)
```

or save the script as `_jz` in a directory on `$fpath`:

```console
$ jz completion zsh > ~/.zfunc/_jz      # with fpath=(~/.zfunc $fpath) before compinit
```

The script asks the `jz` it finds on `PATH`, so it follows the version
you have installed, and a definition added to a registry is offered from
the next Tab on.
