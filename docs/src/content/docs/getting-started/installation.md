---
title: Installation
description: Build and run t3s against a Talos Linux cluster.
---

## Requirements

- Go 1.26.3
- Access to a Talos v1.13.3 cluster
- A valid Talos configuration with at least one context

## Build from source

```bash
git clone https://github.com/m11s-io/t3s.git
cd t3s
go build ./cmd/t3s
```

Run the test suite before using a development build:

```bash
go test ./...
go test -race ./...
```

## Run

```bash
go run ./cmd/t3s --context <name>
```

The context must exist in the Talos configuration resolved by the Talos client. See [Talos configuration](../talos-configuration/) for file selection.
