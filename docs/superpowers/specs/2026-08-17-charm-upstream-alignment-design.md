# Charm Upstream Alignment Design

## Goal

Remove duplicated terminal-key workarounds, make log rendering safe and
non-blocking for terminal-controlled input, and consolidate repeated table
width logic without changing t9s's architecture or k9s-compatible visual
output.

## Scope

All production changes are confined to `internal/tui`. Domain values,
application messages and effects, ports, and Talos adapters remain unchanged.
The design covers three related improvements:

1. use Bubble Tea's canonical textual key representation throughout TUI input
   dispatch;
2. sanitize and wrap log lines with the pinned `x/ansi` primitives; and
3. share the repeated table column-width and cell-writing mechanics while
   retaining screen-specific rendering.

## Key Input

Every TUI handler that dispatches a `tea.KeyPressMsg` derives its binding value
with `message.String()` instead of `message.Keystroke()`. Bubble Tea v2.0.6
delegates this method to ultraviolet's `Key.String`, which returns associated
text when present and falls back to the modifier-preserving keystroke. This
canonically maps both legacy uppercase input and Kitty-protocol Shift+letter
events to the same uppercase binding.

Each update method remains a flat switch local to its screen. Printable filter
input continues to append `message.Text`, preserving composed and
international text. The duplicated Shift+G normalization blocks and the
special C/R/X dual checks are removed, along with comments describing those
now-unnecessary workarounds.

The migration applies consistently to TUI binding dispatch rather than only
the four affected uppercase bindings. This prevents screens from mixing two
different Bubble Tea matching semantics. It does not introduce Bubbles
`key.Binding` maps: the current static, linear switches are simpler and more
greppable, while bindings do not require runtime enablement or generated help.

## Log Safety and Wrapping

Talos log content is untrusted terminal input. Each line is sanitized with
`ansi.Strip` before filtering, measuring, truncating, or wrapping. t9s does not
promise to preserve service-provided terminal styling, and removing escape
sequences prevents cluster-controlled content from changing terminal
presentation.

Filtering operates on the sanitized text shown to the user. In unwrapped mode,
long lines continue to use `ansi.Truncate` with an ellipsis. In wrapped mode,
`ansi.Hardwrap(line, width, true)` performs display-width-aware hard wrapping;
its result is split on newlines and appended to the rendered lines.

This replaces the current loop that truncates a string and then tries to
remove the rendered fragment with `strings.TrimPrefix`. An ANSI-aware
truncation result is not guaranteed to be a literal byte prefix, so that loop
can fail to make progress. Widths at or below zero retain the current
no-wrapping behavior and never call `Hardwrap` with an invalid width.

## Shared Table Mechanics

A small unexported utility in `internal/tui` owns only the mechanics repeated
across the nodes, services, events, etcd, processes, disks, network, problems,
and resource tables:

- initialize each column to its minimum width;
- allocate positive surplus width to the designated grow column; and
- write cells with one-column spacing, `ansi.Truncate(..., "…")`, and
  `ansi.StringWidth`-based padding.

The helper accepts layout metadata and row strings; it has no dependency on
domain values, application state, or a particular screen model. Each screen
continues to own:

- its column definitions and default table width;
- extraction and formatting of domain values;
- visible-row windowing and cursor state;
- selection and marking prefixes;
- k9s skin application; and
- its render function and file-local model.

Default-width fallback remains at the screen call site so the helper has one
unambiguous contract: allocate the supplied non-negative width. The resulting
widths and rendered rows must be byte-for-byte compatible with the existing
golden fixtures.

Replacing the custom tables with `bubbles/table.View` is explicitly out of
scope. Its renderer would require a broader rewrite to reproduce t9s's
selection markers, windowing, and exact k9s layout.

## Error Handling and Concurrency

The changes introduce no I/O, goroutines, commands, effects, or new error
channels. Key handling remains in Bubble Tea updates, log transformation
remains a pure view calculation, and table helpers remain pure formatting
functions. The application reducer and Runner lifecycle are unaffected.

Log sanitization is deterministic and fail-closed: unsupported or malformed
escape sequences do not become terminal presentation instructions. Wrapping
must make progress for every input and cannot contain an input-dependent loop.

## Testing

Key tests cover both legacy uppercase messages and Kitty-style shifted
lowercase messages through the same `String()` dispatch path. Existing
navigation, filtering, log-clear, reboot, and shutdown tests remain the main
behavior contract. A representative shifted-punctuation and control-key case
guards the broader dispatch migration.

Log tests cover:

- long plain text in wrapped and unwrapped modes;
- wide Unicode graphemes and display-width limits;
- ANSI sequences before and beyond a wrap boundary;
- removal of terminal-control sequences from visible and filterable text; and
- completion of wrapping for adversarial ANSI-bearing input.

Shared table-helper tests cover minimum widths, surplus allocation, narrow
widths, the grow-column contract, and ANSI/wide-character truncation and
padding. Existing per-screen and golden tests must remain unchanged unless a
test directly asserts the removed implementation detail rather than visible
behavior.

## Acceptance Criteria

- No TUI binding dispatch uses `Keystroke()` where `String()` provides the
  canonical textual binding.
- No local Shift+letter normalization workaround remains.
- Service log content cannot retain terminal escape sequences in rendered or
  filtered text.
- Wrapped log rendering uses `ansi.Hardwrap` and has no manual
  truncate-and-consume loop.
- Repeated table width allocation and cell writing are centralized without
  changing screen ownership or golden output.
- `go build ./...`, `go vet ./...`, and `go test ./... -race` pass.

