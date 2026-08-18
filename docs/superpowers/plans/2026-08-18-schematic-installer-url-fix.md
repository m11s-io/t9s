# Canonical Schematic Installer URL Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Talos upgrade suggestions from the validated Image Factory host, runtime platform, live schematic, and target version.

**Architecture:** Keep all Talos resource parsing inside the adapter. Read the legacy schematic ExtensionStatus plus PlatformMetadata available in Talos v1.13.3, validate both, and mirror Talos `images.NewInstallerImage`; fall back to the declared installer image when any required metadata is unavailable or invalid.

**Tech Stack:** Go 1.26.3, Talos machinery v1.13.3, COSI runtime resources, `net/url`, Testify.

## Global Constraints

- Never infer a factory repository or platform from the declared installer image.
- Accept only an HTTPS factory URL with a host and no credentials, query, fragment, or non-root path.
- Accept only lowercase ASCII platform identifiers containing letters, digits, and hyphens.
- Preserve digest references in the declared-image fallback.
- Worker-2 must resolve to `factory.talos.dev/metal-installer/75859b9f9a0bc974287be95a622cc7db6f642581a51435cb87eab7e07df8e673:v1.13.4`.

---

### Task 1: Build the Canonical Factory Installer Reference

**Files:**
- Modify: `internal/adapters/talos/node_controller.go`
- Test: `internal/adapters/talos/node_controller_test.go`

**Interfaces:**
- Consumes: schematic ExtensionStatus metadata, `PlatformMetadata.platform`, declared installer image, and running Talos tag.
- Produces: adapter-private schematic metadata and `deriveSchematicInstallerImage(factoryURL, platform, schematic, tag string) string`.

- [ ] **Step 1: Write failing regression tests**

Add an end-to-end `currentInstallImage` unit test with these literal inputs:

```go
declared := "ghcr.io/siderolabs/installer:v1.13.0"
author := "Image Factory (https://factory.talos.dev/)"
platform := "metal"
schematic := "75859b9f9a0bc974287be95a622cc7db6f642581a51435cb87eab7e07df8e673"
want := "factory.talos.dev/metal-installer/" + schematic + ":v1.13.4"
```

Add table cases for AWS, a valid custom HTTPS factory, malformed schemes, credentials, paths, queries, fragments, uppercase/unsafe platforms, empty schematic metadata, and declared-image fallback.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
go test ./internal/adapters/talos -run "TestCurrentInstallImageUsesCanonicalFactoryMetadata|TestDeriveSchematicInstallerImage"
```

Expected: FAIL because the current helper derives the repository from `ghcr.io/siderolabs/installer` and cannot consume runtime platform metadata.

- [ ] **Step 3: Implement the minimal adapter change**

Extend `installImageLookup` to return schematic ID plus author and runtime platform. Parse the final parenthesized author URL with `net/url`, validate the HTTPS root URL, validate the platform and schematic as OCI path components, and return `<host>/<platform>-installer/<schematic>:<tag>`. Keep `deriveUpgradeImage(declared, tag)` as the only fallback.

- [ ] **Step 4: Run focused and full verification**

Run:

```bash
go test ./internal/adapters/talos
go test -race ./internal/adapters/talos
go test ./...
go test -race ./...
go vet ./...
env GOMAXPROCS=2 go build -p 1 ./cmd/t9s
```

Expected: all commands exit zero.

- [ ] **Step 5: Commit and push**

```bash
git add internal/adapters/talos/node_controller.go internal/adapters/talos/node_controller_test.go
git commit -m "fix: derive canonical Talos factory installer"
git push origin main
```
