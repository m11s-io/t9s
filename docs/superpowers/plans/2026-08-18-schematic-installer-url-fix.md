# Schematic Installer URL Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent malformed Talos upgrade suggestions by preserving the declared OCI installer repository while replacing only its schematic and version.

**Architecture:** Keep parsing inside the Talos adapter. ExtensionStatus supplies only the live schematic ID; the declared install image supplies the registry and installer repository. Invalid or non-OCI-shaped inputs fall back to the existing declared-image behavior.

**Tech Stack:** Go 1.26.3, Talos machinery v1.13.3, Testify.

## Global Constraints

- Never derive an OCI registry or installer flavor from ExtensionStatus author text.
- Preserve installer repositories and custom registries from the declared image.
- Reject schematic rewriting for schemes, whitespace, digests, and structurally incomplete references.
- Keep digest references unchanged.

---

### Task 1: Derive a Valid Schematic Installer Reference

**Files:**
- Modify: `internal/adapters/talos/node_controller.go`
- Test: `internal/adapters/talos/node_controller_test.go`

**Interfaces:**
- Consumes: declared installer image, live schematic ID, and running Talos tag.
- Produces: `deriveSchematicInstallerImage(declaredImage, schematic, tag string) string`.

- [ ] **Step 1: Write failing regression tests**

Add table-driven tests asserting:

```go
assert.Equal(t,
    "factory.talos.dev/installer/live-id:v1.13.4",
    deriveSchematicInstallerImage("factory.talos.dev/installer/old-id:v1.13.3", "live-id", "v1.13.4"),
)
```

Cover `metal-installer`, `aws-installer`, a custom registry/repository, and empty results for `https://...`, whitespace, digest references, and missing path segments. Add a `currentInstallImage` regression using author metadata `Image Factory (https://factory.talos.dev/)` and prove the author cannot affect the result.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
go test ./internal/adapters/talos -run 'TestDeriveSchematicInstallerImage|TestCurrentInstallImageIgnoresSchematicAuthor'
```

Expected: FAIL because the current helper accepts factory/flavor inputs and creates the malformed repository.

- [ ] **Step 3: Implement the minimal parser**

Change schematic lookup to return only the live schematic ID. Implement `deriveSchematicInstallerImage` by rejecting schemes, whitespace, digests, empty values, and references with fewer than three slash-separated components; remove the final `<schematic>[:tag]` component from the declared image and append `<live-schematic>:<running-tag>`. Keep `deriveUpgradeImage` as the fallback.

- [ ] **Step 4: Run focused and full verification**

Run:

```bash
go test ./internal/adapters/talos
go test -race ./internal/adapters/talos
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/t9s
```

Expected: all commands exit zero.

- [ ] **Step 5: Commit and push**

```bash
git add internal/adapters/talos/node_controller.go internal/adapters/talos/node_controller_test.go
git commit -m "fix: preserve Talos installer repository"
git push origin main
```
