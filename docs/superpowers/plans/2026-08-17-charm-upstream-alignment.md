# Charm Upstream Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace duplicated key-protocol workarounds and unsafe manual log wrapping with the pinned Charm primitives, then centralize repeated table layout mechanics without changing k9s-compatible output.

**Architecture:** Keep every change inside `internal/tui`: screen-local update switches continue to own input dispatch, `logsModel` continues to own pure log projection, and screen renderers continue to own domain formatting and k9s styling. Introduce one small generic table-layout unit for shared width, header/value extraction, and cell writing; do not change application messages, effects, ports, adapters, or screen ownership.

**Tech Stack:** Go 1.26.3; Bubble Tea v2.0.6; Bubbles v2.1.0; Lip Gloss v2.0.5; `github.com/charmbracelet/x/ansi` v0.11.7; Testify v1.11.1.

## Global Constraints

- Preserve the dependency direction documented in `CONTRIBUTING.md`; Talos SDK types must remain inside `internal/adapters/talos`.
- Keep each screen's flat, greppable `update()` switch and one-model-per-screen convention.
- Preserve printable filter input through `tea.KeyPressMsg.Text`.
- Preserve existing k9s layout, selection markers, windowing, styling, and golden output byte-for-byte.
- Do not replace the custom renderers with `bubbles/table.View`.
- Do not add dependencies or change pinned dependency versions.
- Do not modify the existing untracked `t9s` file.
- Every task must leave its focused tests green; final acceptance is `go build ./...`, `go vet ./...`, and `go test ./... -race`.

---

### Task 1: Canonical Bubble Tea Key Dispatch

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/contexts.go`
- Modify: `internal/tui/nodes.go`
- Modify: `internal/tui/services.go`
- Modify: `internal/tui/events.go`
- Modify: `internal/tui/etcd.go`
- Modify: `internal/tui/processes.go`
- Modify: `internal/tui/disks.go`
- Modify: `internal/tui/network.go`
- Modify: `internal/tui/problems.go`
- Modify: `internal/tui/resources.go`
- Modify: `internal/tui/logs.go`
- Modify: `internal/tui/nodes_test.go`
- Test: `internal/tui/actions_flow_test.go`
- Test: `internal/tui/logs_test.go`
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `tea.KeyPressMsg.String() string` for binding dispatch and `tea.KeyPressMsg.Text` for printable filter input.
- Produces: no new exported or package-level interface; all TUI key dispatch consistently uses Bubble Tea's textual representation.

- [ ] **Step 1: Update the key-event characterization test**

Replace the implementation-specific `Keystroke()` explanation above `shiftKeyPress` in `internal/tui/nodes_test.go` with a canonical-representation test:

```go
func TestKeyStringCanonicalizesLegacyAndKittyUppercase(t *testing.T) {
	legacy := keyPress('G')
	kitty := shiftKeyPress('G')

	assert.Equal(t, "G", legacy.String())
	assert.Equal(t, "G", kitty.String())
	assert.Equal(t, "shift+g", kitty.Keystroke(), "the fixture must exercise the Kitty modifier encoding")
}
```

Retain `shiftKeyPress`, but describe it only as the realistic Kitty-protocol fixture used by uppercase-binding tests.

- [ ] **Step 2: Run the existing behavioral tests as a refactor baseline**

Run:

```bash
go test ./internal/tui -run 'Test(KeyStringCanonicalizesLegacyAndKittyUppercase|NodesGotoBottomHandlesKittyShiftEncoding|RebootKeyHandlesKittyShiftEncoding|ShutdownKeyHandlesKittyShiftEncoding|ServiceLogsFilterFollowWrapAndClear|QuitKeysReturnQuitCommand)$'
```

Expected: PASS. This task is a behavior-preserving dependency-idiom refactor; the baseline proves Bubble Tea itself supplies the normalization before removing the local workaround.

- [ ] **Step 3: Migrate screen-local dispatch values**

In every screen update that currently begins with:

```go
key := message.Keystroke()
```

replace it with:

```go
key := message.String()
```

Delete every block of this form and its Kitty-workaround comment:

```go
if key == "shift+g" || message.Text == "G" {
	key = "G"
}
```

In `logsModel.update`, dispatch clear with the same canonical `key`:

```go
if key == "C" {
	m.clearRequested = true
}
```

Do not replace `message.Text` where it is passed to `printableText`; text entry and binding dispatch have different purposes.

- [ ] **Step 4: Migrate root and context dispatch without adding a helper**

At the start of `model.Update`'s `tea.KeyPressMsg` case (immediately after `m.splash = false`), compute:

```go
key := message.String()
```

This case is a single contiguous block from `case tea.KeyPressMsg:` to the case's final `return m, nil` (currently spanning the ctrl+c check, the pending-action `y` confirmation, the `esc`/history-pop check, the `m.contexts.active` and `m.palette.active` branches, two `switch message.Keystroke() { ... }` statements — one inside the palette branch, one for the root `":"/"q"/"?"/"r"` bindings — and every subsequent `if message.Keystroke() == ...` check across the per-view blocks for `enter`/`d`/`l`/`r`/`p`/`k`/`n`/`space`). Replace **every** `message.Keystroke()` occurrence in that whole case with `key`, including both `switch` statements' tag expressions (`switch message.Keystroke() {` becomes `switch key {` in both places). Do not touch `message.Text` uses (filter input, R/X below) or any `message.Keystroke()` outside this case (e.g. inside `contextsModel.update`, handled separately below).

Replace the R/X dual checks with flat canonical checks:

```go
if key == "R" && m.application.WritesEnabled && !m.nodes.filtering {
	if targets := m.nodes.actionTargets(); len(targets) > 0 {
		var effect application.Effect
		m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionReboot, Targets: targets})
		return m, m.command(effect)
	}
}
if key == "X" && m.application.WritesEnabled && !m.nodes.filtering {
	if targets := m.nodes.actionTargets(); len(targets) > 0 {
		var effect application.Effect
		m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionShutdown, Targets: targets})
		return m, m.command(effect)
	}
}
```

Delete the R/X Kitty-workaround comment. In `contextsModel.update`, switch directly on `message.String()` because no handler-wide local value is otherwise needed.

- [ ] **Step 5: Verify no modifier-preserving dispatch remains**

Run:

```bash
rg -n 'Keystroke\(\)|shift\+[gcrx]|message\.Text == "[GCRX]"' internal/tui --glob '*.go'
```

Expected: only the deliberate `kitty.Keystroke()` assertion in `nodes_test.go`; no production matches.

- [ ] **Step 6: Run the focused and full TUI tests**

Run:

```bash
go test ./internal/tui -run 'Test(KeyStringCanonicalizesLegacyAndKittyUppercase|NodesGotoBottomHandlesKittyShiftEncoding|RebootKey|ShutdownKey|ServiceLogs|QuitKeys)'
go test ./internal/tui
```

Expected: PASS for both commands.

- [ ] **Step 7: Commit the canonical key migration**

```bash
git add internal/tui/model.go internal/tui/contexts.go internal/tui/nodes.go internal/tui/services.go internal/tui/events.go internal/tui/etcd.go internal/tui/processes.go internal/tui/disks.go internal/tui/network.go internal/tui/problems.go internal/tui/resources.go internal/tui/logs.go internal/tui/nodes_test.go
git commit -m "refactor(tui): use canonical Bubble Tea key strings"
```

---

### Task 2: Safe Log Sanitization and Wrapping

**Files:**
- Modify: `internal/tui/logs.go`
- Modify: `internal/tui/logs_test.go`

**Interfaces:**
- Consumes: `ansi.Strip(string) string`, `ansi.StringWidth(string) int`, `ansi.Truncate(string, int, string) string`, and `ansi.Hardwrap(string, int, bool) string` from x/ansi v0.11.7.
- Produces: `func sanitizeLogLines(lines []string) []string`, an unexported pure copy-and-sanitize boundary used before filtering and rendering.

- [ ] **Step 1: Write failing sanitization and wrapping tests**

Add the x/ansi import to `internal/tui/logs_test.go`, then add:

```go
func TestServiceLogsStripANSIBeforeFilteringAndRendering(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"\x1b[31mcritical\x1b[0m service failure",
	}})
	logs.filter = "critical"

	lines := logs.visibleLines()
	require.Equal(t, []string{"critical service failure"}, lines)
	assert.NotContains(t, logs.viewSized(contentSize{Width: 40, Height: 3}), "\x1b[")
}

func TestServiceLogsHardwrapANSIAndWideTextWithinDisplayWidth(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"abcde\x1b[31mfghij\x1b[0m界界",
	}})
	logs.wrap = true

	rendered := logs.renderedLines(6)

	require.NotEmpty(t, rendered)
	for _, line := range rendered {
		assert.LessOrEqual(t, ansi.StringWidth(line), 6)
		assert.NotContains(t, line, "\x1b[")
	}
	assert.Equal(t, "abcdefghij界界", strings.Join(rendered, ""))
}
```

- [ ] **Step 2: Run the new tests to verify the current implementation fails**

Run:

```bash
go test ./internal/tui -run 'TestServiceLogs(StripANSIBeforeFilteringAndRendering|HardwrapANSIAndWideTextWithinDisplayWidth)$' -count=1 -timeout=5s
```

Expected: FAIL because `visibleLines` returns ANSI-bearing text; the timeout also guards the current truncate-and-`TrimPrefix` loop if it cannot make progress.

- [ ] **Step 3: Add the pure sanitization boundary**

Add this helper in `logs.go`:

```go
func sanitizeLogLines(lines []string) []string {
	result := make([]string, len(lines))
	for index, line := range lines {
		result[index] = ansi.Strip(line)
	}
	return result
}
```

Change `visibleLines` to start with sanitized input and return it directly when no filter is active:

```go
func (m logsModel) visibleLines() []string {
	lines := sanitizeLogLines(m.state.Lines)
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return lines
	}
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), query) {
			filtered = append(filtered, line)
		}
	}
	return filtered
}
```

This intentionally copies rather than mutating application state during a pure view projection.

- [ ] **Step 4: Replace manual wrapping with x/ansi Hardwrap**

Replace the wrapped branch and its truncate/consume loop in `renderedLines` with:

```go
wrapped := ansi.Hardwrap(line, width, true)
result = append(result, strings.Split(wrapped, "\n")...)
```

Keep the existing `width <= 0` fast path and the unwrapped `ansi.Truncate(line, width, "…")` branch. Do not add a goroutine, timeout, or mutable cache to rendering.

- [ ] **Step 5: Run log tests and the race-enabled TUI package**

Run:

```bash
go test ./internal/tui -run 'TestServiceLogs' -count=1
go test ./internal/tui -race
```

Expected: PASS. The new adversarial test must complete without relying on the test timeout.

- [ ] **Step 6: Commit safe log rendering**

```bash
git add internal/tui/logs.go internal/tui/logs_test.go
git commit -m "fix(tui): sanitize and hard-wrap service logs"
```

---

### Task 3: Shared Generic Table Layout Mechanics

**Files:**
- Create: `internal/tui/table_layout.go`
- Create: `internal/tui/table_layout_test.go`
- Modify: `internal/tui/nodes_table.go`
- Modify: `internal/tui/services.go`
- Modify: `internal/tui/events.go`
- Modify: `internal/tui/etcd.go`
- Modify: `internal/tui/processes.go`
- Modify: `internal/tui/disks.go`
- Modify: `internal/tui/network.go`
- Modify: `internal/tui/problems.go`
- Modify: `internal/tui/resources.go`

**Interfaces:**
- Produces: `type tableColumn[T any]`, `func calculateColumnWidths[T any](width int, columns []tableColumn[T]) []int`, `func tableHeaders[T any](columns []tableColumn[T]) []string`, `func tableRowValues[T any](value T, columns []tableColumn[T]) []string`, and `func writeTableCells(output *strings.Builder, values []string, widths []int)`.
- Consumes: the existing package constants `selectionWidth` and `columnSpacing`, plus x/ansi display-width primitives.

- [ ] **Step 1: Write failing tests for the shared contracts**

Create `internal/tui/table_layout_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tableLayoutFixture struct {
	name string
	role string
}

func TestCalculateColumnWidthsUsesMinimumsAndGrowsOneColumn(t *testing.T) {
	columns := []tableColumn[tableLayoutFixture]{
		{header: "NAME", minWidth: 4, grow: true, value: func(row tableLayoutFixture) string { return row.name }},
		{header: "ROLE", minWidth: 4, value: func(row tableLayoutFixture) string { return row.role }},
	}

	assert.Equal(t, []int{4, 4}, calculateColumnWidths(5, columns), "narrow layouts retain current minimum widths")
	assert.Equal(t, []int{9, 4}, calculateColumnWidths(selectionWidth+columnSpacing+4+4+5, columns))
	assert.Equal(t, []string{"NAME", "ROLE"}, tableHeaders(columns))
	assert.Equal(t, []string{"control-plane", "cp"}, tableRowValues(tableLayoutFixture{name: "control-plane", role: "cp"}, columns))
}

func TestWriteTableCellsUsesDisplayWidthForTruncationAndPadding(t *testing.T) {
	var output strings.Builder
	writeTableCells(&output, []string{"界界界", "\x1b[31mhealthy\x1b[0m"}, []int{5, 4})

	rendered := output.String()
	require.Equal(t, 10, ansi.StringWidth(rendered))
	assert.Contains(t, ansi.Strip(rendered), "界界…")
	assert.Contains(t, ansi.Strip(rendered), "hea…")
}
```

- [ ] **Step 2: Run the shared-helper tests to verify they fail to compile**

Run:

```bash
go test ./internal/tui -run 'Test(CalculateColumnWidths|WriteTableCells)' -count=1
```

Expected: FAIL with undefined `tableColumn`, `calculateColumnWidths`, `tableHeaders`, `tableRowValues`, and `writeTableCells`.

- [ ] **Step 3: Implement the minimal generic table unit**

Create `internal/tui/table_layout.go`:

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type tableColumn[T any] struct {
	header   string
	minWidth int
	value    func(T) string
	grow     bool
}

func calculateColumnWidths[T any](width int, columns []tableColumn[T]) []int {
	widths := make([]int, len(columns))
	used := selectionWidth + columnSpacing*max(0, len(columns)-1)
	growIndex := -1
	for index, column := range columns {
		widths[index] = column.minWidth
		used += column.minWidth
		if column.grow {
			growIndex = index
		}
	}
	if remaining := width - used; remaining > 0 && growIndex >= 0 {
		widths[growIndex] += remaining
	}
	return widths
}

func tableHeaders[T any](columns []tableColumn[T]) []string {
	values := make([]string, len(columns))
	for index, column := range columns {
		values[index] = column.header
	}
	return values
}

func tableRowValues[T any](value T, columns []tableColumn[T]) []string {
	values := make([]string, len(columns))
	for index, column := range columns {
		values[index] = column.value(value)
	}
	return values
}

func writeTableCells(output *strings.Builder, values []string, widths []int) {
	for index, value := range values {
		if index > 0 {
			output.WriteByte(' ')
		}
		cell := ansi.Truncate(value, widths[index], "…")
		output.WriteString(cell)
		if padding := widths[index] - ansi.StringWidth(cell); padding > 0 && index < len(values)-1 {
			output.WriteString(strings.Repeat(" ", padding))
		}
	}
}
```

The `growIndex == -1` guard makes the helper well-defined for fixed-width tables rather than silently growing column zero.

- [ ] **Step 4: Run helper tests to verify the implementation**

Run:

```bash
go test ./internal/tui -run 'Test(CalculateColumnWidths|WriteTableCells)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Migrate each screen's column declaration**

Replace screen-specific column struct types with the shared generic type. For example, nodes becomes:

```go
var nodeColumns = []tableColumn[domain.NodeSnapshot]{
	{header: "NAME", minWidth: 12, grow: true, value: func(node domain.NodeSnapshot) string { return node.DisplayName() }},
	// Preserve the remaining existing declarations exactly.
}
```

Apply the same mechanical type change to service, event, etcd, process, disk, link, problem, resource-kind, and resource-instance column slices. Preserve every current header, minimum width, grow flag, and value function exactly.

- [ ] **Step 6: Delegate screen width and row mechanics**

Keep each screen's default-width fallback, then delegate. The nodes functions become:

```go
func nodeColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultNodesWidth
	}
	return calculateColumnWidths(width, nodeColumns)
}

func nodeHeaders() []string {
	return tableHeaders(nodeColumns)
}

func nodeRowValues(node domain.NodeSnapshot) []string {
	return tableRowValues(node, nodeColumns)
}
```

Replace calls such as `writeNodeCells`, `writeServiceCells`, and `writeResourceCells` with `writeTableCells`. Delete every screen-specific cell writer and duplicated width-allocation loop. Where a screen currently constructs headers or row values inline, replace that loop with `tableHeaders(columns)` or `tableRowValues(value, columns)`.

- [ ] **Step 7: Format and verify table behavior**

Run:

```bash
gofmt -w internal/tui/table_layout.go internal/tui/table_layout_test.go internal/tui/nodes_table.go internal/tui/services.go internal/tui/events.go internal/tui/etcd.go internal/tui/processes.go internal/tui/disks.go internal/tui/network.go internal/tui/problems.go internal/tui/resources.go
go test ./internal/tui -run 'Test(CalculateColumnWidths|WriteTableCells|.*RenderSemanticColumns|NodesRender|ServicesRender|K9sTable|K9sVisual)' -count=1
go test ./internal/tui
```

Expected: PASS, including unchanged golden fixtures.

- [ ] **Step 8: Verify the duplicated mechanics are gone**

Run:

```bash
rg -n 'func write(Node|Service|Event|Etcd|Process|Disk|Link|Problem|Resource)Cells|used := selectionWidth' internal/tui --glob '*.go'
```

Expected: no matches outside `table_layout.go`; only one width-allocation implementation exists.

- [ ] **Step 9: Commit the shared table mechanics**

```bash
git add internal/tui/table_layout.go internal/tui/table_layout_test.go internal/tui/nodes_table.go internal/tui/services.go internal/tui/events.go internal/tui/etcd.go internal/tui/processes.go internal/tui/disks.go internal/tui/network.go internal/tui/problems.go internal/tui/resources.go
git commit -m "refactor(tui): share table layout mechanics"
```

---

### Task 4: Repository Acceptance Verification

**Files:**
- Test only: repository-wide Go packages and working tree state.

**Interfaces:**
- Consumes: the completed changes from Tasks 1-3.
- Produces: verification evidence; no source changes unless a failing command reveals a defect in a prior task.

- [ ] **Step 1: Check formatting and static architecture constraints**

Run:

```bash
gofmt -w internal/tui
rg -n 'github.com/siderolabs|github.com/cosi-project/runtime|talos/pkg/machinery' internal/domain internal/ports internal/application internal/tui
rg -n 'Keystroke\(\)|shift\+[gcrx]|message\.Text == "[GCRX]"' internal/tui --glob '*.go'
```

Expected: the architecture search has no imports outside adapters (test fixture strings containing `talos.dev` are not SDK imports); the key search finds only the deliberate `Keystroke()` fixture assertion.

- [ ] **Step 2: Run the required build and vet checks**

Run:

```bash
go build ./...
go vet ./...
```

Expected: both commands exit successfully with no diagnostics.

- [ ] **Step 3: Run the full race-enabled suite**

Run:

```bash
go test ./... -race -count=1
```

Expected: every package passes under the race detector.

- [ ] **Step 4: Inspect the final diff and working tree**

Run:

```bash
git diff --check
git status --short
git log -4 --oneline
```

Expected: no whitespace errors; only the pre-existing untracked `t9s` file may remain; the three implementation commits and design commit are visible.

