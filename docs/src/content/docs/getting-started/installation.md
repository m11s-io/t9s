---
title: Installation
description: Build and run t9s against a Talos Linux cluster.
---

## Requirements

- Go 1.26.3
- Access to a Talos v1.13.3 cluster
- A valid Talos configuration with at least one context

## Download a release

Prebuilt binaries for Linux and macOS (amd64 and arm64) are published on the
[GitHub Releases](https://github.com/m11s-io/t9s/releases) page for every
tagged version.

```bash
# Substitute the version, OS (linux or darwin), and architecture (amd64 or arm64)
VERSION=0.1.0
OS=linux
ARCH=amd64

curl -LO "https://github.com/m11s-io/t9s/releases/download/v${VERSION}/t9s_${OS}_${ARCH}.tar.gz"
curl -LO "https://github.com/m11s-io/t9s/releases/download/v${VERSION}/checksums.sha256"

sha256sum -c checksums.sha256 --ignore-missing
tar xzf "t9s_${OS}_${ARCH}.tar.gz"
install -m 0755 t9s /usr/local/bin/t9s
```

On macOS, verify the checksum with `shasum -a 256 -c checksums.sha256 --ignore-missing`
instead of `sha256sum`.

Confirm the install with:

```bash
t9s --version
```

## Build from source

```bash
git clone https://github.com/m11s-io/t9s.git
cd t9s
go build ./cmd/t9s
```

Run the test suite before using a development build:

```bash
go test ./...
go test -race ./...
```

## Run

```bash
go run ./cmd/t9s --context <name>
```

The context must exist in the Talos configuration resolved by the Talos client. See [Talos configuration](../talos-configuration/) for file selection.
