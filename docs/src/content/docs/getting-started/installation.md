---
title: Installation
description: Build and run t9s against a Talos Linux cluster.
---

## Requirements

- Go 1.26.3
- Access to a Talos v1.13.3 cluster
- A valid Talos configuration with at least one context

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
