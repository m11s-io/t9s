# Talos Configuration Apply Design

**Status:** Reviewed; implementation plan pending

**Date:** 2026-10-20

**Scope:** Phase 4 configuration preview and apply only

## Goal

Allow an operator to validate a local Talos machine configuration against selected nodes using server-side dry runs, review a safe per-node summary, then explicitly apply the same bytes to those nodes.

## Context and constraints

- t9s is a read-only TUI unless started with `--enable-writes` or `T9S_ENABLE_WRITES`.
- The pinned Talos SDK is v1.14.4. Its `ApplyConfigurationRequest` has a server-side `dry_run` field and the `AUTO`, `NO_REBOOT`, `STAGED`, and `TRY` modes. `REBOOT` is deprecated.
- The roadmap requires server-side dry-run preview before confirmation and asks that the pinned SDK modes be checked before implementation.
- Talos applies configuration independently on each node. A multi-node operation cannot promise atomicity or cross-node rollback.
- Configuration bytes may contain credentials. They must not be rendered, logged, or persisted.

## User flow

1. The operator opens `:config`, enters a local file path, selects one or more nodes, and chooses an apply mode.
2. t9s reads the file locally and requests a server-side dry run from every selected node using those exact bytes and the selected mode.
3. The preview displays the file basename, the first 12 hexadecimal characters of its SHA-256 digest, the canonical target list, the requested mode, each node’s returned mode, and a normalized validation success/failure summary. Raw server mode details, diff text, warning text, and validation errors can contain configuration values and never reach TUI state. The summary explains that configuration changes and warnings are not displayed.
4. If any target cannot preview successfully, the apply action remains blocked. The operator can correct the file or target list and prepare a new preview.
5. If every target previews successfully, t9s asks the operator to type the exact displayed phrase, which includes the selected mode and target count. The preview lists every target, and confirmation is bound to that canonical target list and the prepared configuration digest.
6. t9s applies the same bytes and mode to the same targets, one node at a time, showing per-node progress and outcomes.

The preview is a request to Talos with `dry_run=true`; it is not a local parser or a promise that apply will succeed. Talos v1.14.4 embeds a configuration diff in `ModeDetails` without redacting secrets, and its dry-run response omits the validation warnings computed by the server. Discard those raw strings in the adapter and report only normalized statuses, returned mode, and warning counts when available. Do not label absent warnings as “no warnings.” A redacted diff viewer requires a separate design.

The same complete file is sent unchanged to every selected node. Show this explicitly before confirmation: there is no templating, patch merging, per-node file selection, or preservation of existing node-specific values. The operator must ensure that the file is appropriate for every listed node.

## Apply modes

The initial UI offers `AUTO`, `NO_REBOOT`, and `STAGED`, with behavior verified in the pinned v1.14.4 server source. `REBOOT` is omitted because Talos deprecates it. `TRY` is deferred: Talos applies it temporarily and automatically rolls the node back after a timeout unless another operation commits persistent configuration. That timed lifecycle needs a separate multi-node UX and recovery design.

The selected mode is used for both the dry-run and apply request. The mode is never inferred from configuration content. No default mode is preselected; the operator must choose explicitly.

## Architecture and data flow

- The TUI collects a file path, target selection, and mode. It reads the file locally and passes the bytes to an application operation.
- The application layer owns writes gating, request validation, preview preparation, confirmation binding, one-use prepared plans, the shared mutation lease, operation deadlines, and per-target outcomes.
- A dedicated configuration controller port exposes dry-run preview and apply operations. The Talos adapter maps these to `ApplyConfigurationRequest`, setting `dry_run` to true for preview and false for apply, and maps only returned modes, warning counts, normalized statuses, and private response fingerprints into domain results. Raw server strings remain within the adapter and are discarded.
- Prepared application state retains immutable configuration bytes privately until the plan is applied or discarded. The TUI model receives only a display summary and preview results. The digest is for binding and confirmation, not a replacement for the bytes.
- The Talos adapter is the only layer that imports SDK request/response types.

## Safety and failure behavior

- Both preparation and execution enforce the existing write gate. No apply command is available when writes are disabled.
- A preview is run against every selected target before apply is enabled. Any preview transport, validation, or response error blocks the entire apply operation.
- Confirmation is bound to the prepared bytes, mode, and canonical target list. A prepared plan is single-use.
- Execution is serialized through the existing session-scoped mutation lease. This coordinates operations in that session; it cannot exclude changes from other clients or processes. Targets are applied sequentially, without automatic retry.
- Plans expire five minutes after successful preparation and belong to the session that prepared them. Context changes and session closure invalidate them. Editing the form discards the plan; changing the source file on disk does not change prepared bytes. A new preview is required to use new file contents.
- Before the first apply RPC, re-resolve every canonical node and repeat every dry run under the mutation lease. Bind private SHA-256 fingerprints of the original raw dry-run details and returned modes; a changed identity, failed recheck, or changed fingerprint invalidates confirmation and requires a new preview. These fingerprints stay private. This detects observed drift and cannot eliminate races with external changes.
- Report successful persistent-mode responses as accepted and successful `STAGED` responses as staged; neither proves node health or convergence. Report failures detected before dispatch as failed, errors after dispatch conservatively as unconfirmed, and undispatched targets as not attempted. A nil/empty response, metadata error, or unexpected returned mode is not success. Preserve completed outcomes if a later target fails or the operator cancels.
- Cancellation or connection loss after dispatch may leave a partial configuration update. t9s reports that outcome clearly and requires fresh inspection before another attempt; it does not claim rollback or completion based on reconnecting.
- An apply failure on one node stops dispatch to later nodes. Already-dispatched nodes retain their reported outcomes.
- Accept only nonempty regular files of at most 4 MiB; perform a bounded read and reject other file types. Empty or unreadable files, empty or ambiguous target lists, unsupported modes, and stale or reused plans fail before apply. Deduplicate canonical targets while preserving display/dispatch order.
- Use a 30-second deadline for each preview/apply RPC and a ten-minute overall deadline for execution, preserving any shorter caller deadline. Cancellation stops further dispatch.
- Reject raw preview details exceeding 1 MiB per target rather than truncating the fingerprint input. Reject unsupported or mismatched response modes. For requested `AUTO`, the pinned server resolves the mode to `NO_REBOOT`; requested `NO_REBOOT` and `STAGED` must return the same mode.

## Information handling

- Do not place configuration bytes or raw file contents in TUI model fields, logs, notices, operation history, docs, or telemetry.
- The asynchronous file-read request and adapter call may hold transient bytes. Copy the payload into the private prepared application plan; never put bytes in a Bubble Tea message or TUI model. Clear owned buffers where practical and release references after plan disposal or execution; Go cannot guarantee erasure of every runtime or SDK copy. Do not write configuration files or previews to disk.
- Normalize file, validation, transport, and metadata errors before returning display results; raw errors may echo paths or configuration values. Sanitize terminal control characters in displayed filenames and node labels.
- Display only the file basename and a short digest in the confirmation view. Avoid echoing a full path that could contain sensitive directory names.

## Validation and documentation

Validation should cover the application write gate and plan binding, all-target preview gating, adapter request mapping for dry-run and apply, supported mode mapping (including `AUTO` resolving to `NO_REBOOT`), secret-bearing server diffs/errors, missing warnings, file bounds, plan expiry/disposal, source-file changes, execution-time identity/preview drift, partial/unconfirmed outcomes, cancellation, and TUI confirmation and progress states. Use fake-backed application and adapter tests plus a disposable Talos smoke check before release; do not validate by applying to a live cluster.

Update the command reference and security guide to explain configuration input, supported modes, the write gate, preview behavior, sensitive data handling, and partial multi-node outcomes. Update the Phase 4 roadmap status only when implementation and its required validation are complete.

## Out of scope

- Editing or generating machine configuration inside t9s.
- Bootstrap or etcd recovery.
- Kubeconfig export and curated etcd resource details.
- Kubernetes upgrades through Talos.
- Applying configuration to a live cluster as part of development validation.


## Review evidence and conclusions

Reviewed on 2026-10-20 against the local pinned upstream sources and existing application operation implementation:

- Talos `internal/app/machined/internal/server/v1alpha1/v1alpha1_server.go`, `ApplyConfiguration` and `generateDiff`: dry-run returns mode details containing a diff, omits warnings, and maps `AUTO` to `NO_REBOOT`.
- Machinery `config/configdiff/configdiff.go`, `DiffConfigs`: serializes both configurations without applying secret redaction.
- t9s `internal/application/mutation.go` and `action_plan.go`: admission is session-bound, one-use, write-gated, and protected by a session-scoped mutation lease; configuration support must extend these checks.

The review corrected secret exposure, unavailable warning claims, unspecified plan freshness, and ambiguous partial-outcome semantics. Feature code and disposable-cluster validation remain implementation work.
