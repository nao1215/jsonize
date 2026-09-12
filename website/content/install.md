---
title: Install
description: Install jz from source or from a release archive.
---

## From source

Go 1.26 or later:

```console
$ go install github.com/nao1215/jsonize/cmd/jz@latest
```

## From a release archive

Archives for Linux, macOS and Windows are attached to every release, with
build provenance attested by GitHub:

```console
$ curl -sSfL https://github.com/nao1215/jsonize/releases/latest/download/jsonize_<version>_linux_amd64.tar.gz | tar xz
$ ./jz version
```

The parser registry ships inside the binary, so a release carries both
the code and the definitions and jz never fetches anything at run time.

## Check that it works

```console
$ df -h | jz
$ jz list
```

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
