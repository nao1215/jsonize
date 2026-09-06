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
