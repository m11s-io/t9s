# Service and Talos Node Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Talos service start/stop/restart (Services screen) and Talos OS rollback/upgrade (Nodes screen) to t9s, extending the existing Reboot/Shutdown write-action framework.

**Architecture:** New capabilities on `ports.NodeController` (Rollback, Upgrade, CurrentInstallImage) and a new `ports.ServiceController` (Start, Stop, Restart), implemented in `internal/adapters/talos` against the existing `*talosclient.Client`. The application layer (`internal/application`) extends the existing `PendingAction`/`ActionKind`/confirm-flow machinery for node actions and adds a small parallel `PendingServiceAction` for services (never bulk, so it doesn't share `PendingAction`'s target-list shape). The TUI (`internal/tui`) adds new keybindings gated by `WritesEnabled`, reusing the existing y/n confirm footer, plus one new small component (a prefilled text-input prompt for the Upgrade image).

**Tech Stack:** Go, Bubble Tea v2 (`charm.land/bubbletea/v2`), Bubbles v2 (`charm.land/bubbles/v2/textinput`), `github.com/siderolabs/talos/pkg/machinery` (client + COSI resources), testify (`assert`/`require`).

**Spec:** `docs/superpowers/specs/2026-08-17-service-and-talos-actions-design.md`

## Global Constraints

- No `stage`/`force`/`legacy` upgrade flags — `Upgrade` always calls the non-staged, non-forced path, matching talosctl's own defaults.
- Talos Upgrade always targets exactly the cursor row on the Nodes screen — never the marked set, even if nodes are marked.
- Talos Rollback and Service start/stop/restart are gated by `WritesEnabled`, exactly like the existing Reboot/Shutdown/mark keys.
- Kubernetes control-plane upgrade (`talosctl upgrade-k8s`) is out of scope for this plan entirely.
- Every new write path must go through the existing y/n confirm footer — no action fires without an explicit `y` keypress.
- Run `go build ./...` and `go test ./...` at the end of every task; both must be clean before moving to the next task.

---

### Task 1: NodeController gains Rollback, Upgrade, CurrentInstallImage

**Files:**
- Modify: `internal/ports/node.go`
- Modify: `internal/adapters/talos/node_controller.go`
- Modify: `internal/adapters/talos/node_controller_test.go`
- Modify: `internal/testkit/fakes.go`

**Interfaces:**
- Produces: `ports.NodeController` gains `Rollback(ctx context.Context, target string) error`, `Upgrade(ctx context.Context, target, image string) error`, `CurrentInstallImage(ctx context.Context, target string) (string, error)`.
- Produces: `testkit.FakeNodeController` gains matching `RollbackFunc`, `UpgradeFunc`, `CurrentInstallImageFunc` fields and methods, so later tasks can inject fakes for these three calls.

- [ ] **Step 1: Write the failing adapter tests**

Append to `internal/adapters/talos/node_controller_test.go` (add `upgradeErr error` and `rollbackErr error` fields and an `upgradeReq talosclient.UpgradeOptions` field, plus `currentImage string` / `currentImageErr error`, to `fakeNodeControlClient`, and implement the three new methods on it):

```go
func (c *fakeNodeControlClient) Rollback(ctx context.Context) error {
	return c.rollbackErr
}

func (c *fakeNodeControlClient) Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error {
	for _, opt := range opts {
		opt(&c.upgradeReq)
	}
	return c.upgradeErr
}

func (c *fakeNodeControlClient) CurrentInstallImage(ctx context.Context) (string, error) {
	return c.currentImage, c.currentImageErr
}

func TestNodeControllerRollbackWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{rollbackErr: errors.New("no previous version")}
	controller := newNodeController(client)

	err := controller.Rollback(t.Context(), "cp-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "no previous version")
}

func TestNodeControllerRollbackSucceeds(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Rollback(t.Context(), "cp-1")

	require.NoError(t, err)
}

func TestNodeControllerUpgradeSendsImage(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Upgrade(t.Context(), "cp-1", "ghcr.io/siderolabs/installer:v1.13.3")

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.3", client.upgradeReq.Request.Image)
}

func TestNodeControllerUpgradeWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{upgradeErr: errors.New("incompatible image")}
	controller := newNodeController(client)

	err := controller.Upgrade(t.Context(), "cp-1", "bad:image")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "incompatible image")
}

func TestNodeControllerCurrentInstallImageReturnsImage(t *testing.T) {
	client := &fakeNodeControlClient{currentImage: "ghcr.io/siderolabs/installer:v1.13.2"}
	controller := newNodeController(client)

	image, err := controller.CurrentInstallImage(t.Context(), "cp-1")

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.2", image)
}

func TestNodeControllerCurrentInstallImageWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{currentImageErr: errors.New("resource not found")}
	controller := newNodeController(client)

	_, err := controller.CurrentInstallImage(t.Context(), "cp-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
}
```

Also add the four new fields to the `fakeNodeControlClient` struct declaration at the top of the file:

```go
type fakeNodeControlClient struct {
	rebootReq       machineapi.RebootRequest
	rebootErr       error
	shutdownReq     machineapi.ShutdownRequest
	shutdownErr     error
	rollbackErr     error
	upgradeReq      talosclient.UpgradeOptions
	upgradeErr      error
	currentImage    string
	currentImageErr error
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `go test ./internal/adapters/talos/... 2>&1 | head -30`
Expected: compile errors — `fakeNodeControlClient` does not implement `nodeControlClient` (missing `Rollback`/`Upgrade`/`CurrentInstallImage`), and `newNodeController` / `controller.Rollback` etc. are undefined on `*nodeController`.

- [ ] **Step 3: Extend the port interface**

In `internal/ports/node.go`, extend `NodeController`:

```go
type NodeController interface {
	Reboot(ctx context.Context, target string, mode RebootMode) error
	Shutdown(ctx context.Context, target string, force bool) error
	Rollback(ctx context.Context, target string) error
	Upgrade(ctx context.Context, target, image string) error
	CurrentInstallImage(ctx context.Context, target string) (string, error)
}
```

- [ ] **Step 4: Implement the adapter**

In `internal/adapters/talos/node_controller.go`, extend `nodeControlClient`, `machineryNodeControlClient`, and `nodeController`:

```go
type nodeControlClient interface {
	Reboot(ctx context.Context, opts ...talosclient.RebootMode) error
	Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error
	Rollback(ctx context.Context) error
	Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error
	CurrentInstallImage(ctx context.Context) (string, error)
}
```

Add to `machineryNodeControlClient`:

```go
func (c machineryNodeControlClient) Rollback(ctx context.Context) error {
	return c.client.Rollback(ctx)
}

func (c machineryNodeControlClient) Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error {
	_, err := c.client.UpgradeWithOptions(ctx, opts...)
	return err
}

func (c machineryNodeControlClient) CurrentInstallImage(ctx context.Context) (string, error) {
	cfg, err := safe.StateGet[*talosconfig.MachineConfig](
		ctx, c.client.COSI,
		resource.NewMetadata(talosconfig.NamespaceName, talosconfig.MachineConfigType, talosconfig.ActiveID, resource.VersionUndefined),
	)
	if err != nil {
		return "", err
	}
	return cfg.Provider().Machine().Install().Image(), nil
}
```

Add to `nodeController`:

```go
func (c *nodeController) Rollback(ctx context.Context, target string) error {
	if err := c.client.Rollback(talosclient.WithNode(ctx, target)); err != nil {
		return fmt.Errorf("rollback %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) Upgrade(ctx context.Context, target, image string) error {
	if err := c.client.Upgrade(talosclient.WithNode(ctx, target), talosclient.WithUpgradeImage(image)); err != nil {
		return fmt.Errorf("upgrade %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) CurrentInstallImage(ctx context.Context, target string) (string, error) {
	image, err := c.client.CurrentInstallImage(talosclient.WithNode(ctx, target))
	if err != nil {
		return "", fmt.Errorf("current install image %s: %w", target, err)
	}
	return image, nil
}
```

Update the import block at the top of `node_controller.go` to add:

```go
"github.com/cosi-project/runtime/pkg/resource"
"github.com/cosi-project/runtime/pkg/safe"
talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
```

- [ ] **Step 5: Extend the testkit fake**

In `internal/testkit/fakes.go`, extend `FakeNodeController`:

```go
type FakeNodeController struct {
	RebootFunc              func(ctx context.Context, target string, mode ports.RebootMode) error
	ShutdownFunc            func(ctx context.Context, target string, force bool) error
	RollbackFunc            func(ctx context.Context, target string) error
	UpgradeFunc             func(ctx context.Context, target, image string) error
	CurrentInstallImageFunc func(ctx context.Context, target string) (string, error)
}

func (f *FakeNodeController) Rollback(ctx context.Context, target string) error {
	return f.RollbackFunc(ctx, target)
}

func (f *FakeNodeController) Upgrade(ctx context.Context, target, image string) error {
	return f.UpgradeFunc(ctx, target, image)
}

func (f *FakeNodeController) CurrentInstallImage(ctx context.Context, target string) (string, error) {
	return f.CurrentInstallImageFunc(ctx, target)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS, no compile errors.

- [ ] **Step 7: Commit**

```bash
git add internal/ports/node.go internal/adapters/talos/node_controller.go internal/adapters/talos/node_controller_test.go internal/testkit/fakes.go
git commit -m "feat(talos): add Rollback, Upgrade, and CurrentInstallImage to NodeController"
```

---

### Task 2: ServiceController port, adapter, and session wiring

**Files:**
- Create: `internal/ports/service_controller.go`
- Modify: `internal/ports/context.go`
- Create: `internal/adapters/talos/service_controller.go`
- Create: `internal/adapters/talos/service_controller_test.go`
- Modify: `internal/adapters/talos/session.go`
- Modify: `internal/testkit/fakes.go`

**Interfaces:**
- Consumes: none from Task 1 (independent port).
- Produces: `ports.ServiceController` with `Start`, `Stop`, `Restart(ctx context.Context, node, service string) error`. `ports.Session` gains `ServiceActions() ServiceController`. `testkit.FakeServiceController` and `testkit.FakeSession.ServiceController` for later application-layer tests.

- [ ] **Step 1: Write the failing adapter test**

Create `internal/adapters/talos/service_controller_test.go`:

```go
package talos

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeServiceControlClient struct {
	startID    string
	startErr   error
	stopID     string
	stopErr    error
	restartID  string
	restartErr error
}

func (c *fakeServiceControlClient) Start(ctx context.Context, id string) error {
	c.startID = id
	return c.startErr
}

func (c *fakeServiceControlClient) Stop(ctx context.Context, id string) error {
	c.stopID = id
	return c.stopErr
}

func (c *fakeServiceControlClient) Restart(ctx context.Context, id string) error {
	c.restartID = id
	return c.restartErr
}

func TestServiceControllerStartSendsServiceID(t *testing.T) {
	client := &fakeServiceControlClient{}
	controller := newServiceController(client)

	err := controller.Start(t.Context(), "cp-1", "etcd")

	require.NoError(t, err)
	assert.Equal(t, "etcd", client.startID)
}

func TestServiceControllerStartWrapsError(t *testing.T) {
	client := &fakeServiceControlClient{startErr: errors.New("unreachable")}
	controller := newServiceController(client)

	err := controller.Start(t.Context(), "cp-1", "etcd")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "etcd")
	assert.Contains(t, err.Error(), "cp-1")
}

func TestServiceControllerStopSendsServiceID(t *testing.T) {
	client := &fakeServiceControlClient{}
	controller := newServiceController(client)

	err := controller.Stop(t.Context(), "cp-1", "etcd")

	require.NoError(t, err)
	assert.Equal(t, "etcd", client.stopID)
}

func TestServiceControllerRestartSendsServiceID(t *testing.T) {
	client := &fakeServiceControlClient{}
	controller := newServiceController(client)

	err := controller.Restart(t.Context(), "cp-1", "etcd")

	require.NoError(t, err)
	assert.Equal(t, "etcd", client.restartID)
}

func TestServiceControllerRestartWrapsError(t *testing.T) {
	client := &fakeServiceControlClient{restartErr: errors.New("service not found")}
	controller := newServiceController(client)

	err := controller.Restart(t.Context(), "cp-1", "unknown")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
	assert.Contains(t, err.Error(), "cp-1")
}
```

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./internal/adapters/talos/... 2>&1 | head -30`
Expected: compile error — `newServiceController` undefined.

- [ ] **Step 3: Add the port**

Create `internal/ports/service_controller.go`:

```go
package ports

import "context"

type ServiceController interface {
	Start(ctx context.Context, node, service string) error
	Stop(ctx context.Context, node, service string) error
	Restart(ctx context.Context, node, service string) error
}
```

In `internal/ports/context.go`, add to the `Session` interface (after `NodeActions() NodeController`):

```go
	NodeActions() NodeController
	ServiceActions() ServiceController
```

- [ ] **Step 4: Implement the adapter**

Create `internal/adapters/talos/service_controller.go`:

```go
package talos

import (
	"context"
	"fmt"

	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type serviceControlClient interface {
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
}

type machineryServiceControlClient struct{ client *talosclient.Client }

func (c machineryServiceControlClient) Start(ctx context.Context, id string) error {
	_, err := c.client.ServiceStart(ctx, id)
	return err
}

func (c machineryServiceControlClient) Stop(ctx context.Context, id string) error {
	_, err := c.client.ServiceStop(ctx, id)
	return err
}

func (c machineryServiceControlClient) Restart(ctx context.Context, id string) error {
	_, err := c.client.ServiceRestart(ctx, id)
	return err
}

type serviceController struct{ client serviceControlClient }

func newServiceController(client serviceControlClient) ports.ServiceController {
	return &serviceController{client: client}
}

func (c *serviceController) Start(ctx context.Context, node, service string) error {
	if err := c.client.Start(talosclient.WithNode(ctx, node), service); err != nil {
		return fmt.Errorf("start %s@%s: %w", service, node, err)
	}
	return nil
}

func (c *serviceController) Stop(ctx context.Context, node, service string) error {
	if err := c.client.Stop(talosclient.WithNode(ctx, node), service); err != nil {
		return fmt.Errorf("stop %s@%s: %w", service, node, err)
	}
	return nil
}

func (c *serviceController) Restart(ctx context.Context, node, service string) error {
	if err := c.client.Restart(talosclient.WithNode(ctx, node), service); err != nil {
		return fmt.Errorf("restart %s@%s: %w", service, node, err)
	}
	return nil
}
```

- [ ] **Step 5: Wire the session**

In `internal/adapters/talos/session.go`, add a field to `session` and construct it in `Open`:

```go
	return &session{
		client:            client,
		nodes:             newNodeReader(&machineryAPI{client: client}, time.Now),
		nodeController:    newNodeController(machineryNodeControlClient{client: client}),
		serviceController: newServiceController(machineryServiceControlClient{client: client}),
		services:          newServiceReader(client, time.Now),
		...
```

```go
type session struct {
	client            *talosclient.Client
	nodes             ports.NodeReader
	nodeController    ports.NodeController
	serviceController ports.ServiceController
	services          ports.ServiceReader
	...
```

Add the accessor method next to `NodeActions`:

```go
func (s *session) ServiceActions() ports.ServiceController { return s.serviceController }
```

- [ ] **Step 6: Extend the testkit fakes**

In `internal/testkit/fakes.go`, add after `FakeNodeController`:

```go
type FakeServiceController struct {
	StartFunc   func(ctx context.Context, node, service string) error
	StopFunc    func(ctx context.Context, node, service string) error
	RestartFunc func(ctx context.Context, node, service string) error
}

func (f *FakeServiceController) Start(ctx context.Context, node, service string) error {
	return f.StartFunc(ctx, node, service)
}

func (f *FakeServiceController) Stop(ctx context.Context, node, service string) error {
	return f.StopFunc(ctx, node, service)
}

func (f *FakeServiceController) Restart(ctx context.Context, node, service string) error {
	return f.RestartFunc(ctx, node, service)
}
```

Add a field to `FakeSession` (next to `NodeController ports.NodeController`) and its accessor:

```go
	NodeController    ports.NodeController
	ServiceController ports.ServiceController
```

```go
func (f *FakeSession) ServiceActions() ports.ServiceController { return f.ServiceController }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ports/service_controller.go internal/ports/context.go internal/adapters/talos/service_controller.go internal/adapters/talos/service_controller_test.go internal/adapters/talos/session.go internal/testkit/fakes.go
git commit -m "feat(talos): add ServiceController port, adapter, and session wiring"
```

---

### Task 3: Application layer — ActionRollback and ActionUpgrade

**Files:**
- Modify: `internal/application/model.go`
- Modify: `internal/application/actions.go`
- Modify: `internal/application/update.go`
- Modify: `internal/application/actions_test.go`
- Modify: `internal/application/update_test.go`

**Interfaces:**
- Consumes: `ports.NodeController.Rollback`/`.Upgrade` (Task 1).
- Produces: `application.ActionRollback`, `application.ActionUpgrade` (`ActionKind` values); `PendingAction.Image string`; `RequestAction.Image string`. `BuildActionEffects` behavior extended — signature unchanged.

- [ ] **Step 1: Write the failing tests**

Append to `internal/application/actions_test.go`:

```go
func TestBuildActionEffectsUsesRollbackForRollbackKind(t *testing.T) {
	var rollbackCalled bool
	controller := &testkit.FakeNodeController{
		RollbackFunc: func(context.Context, string) error {
			rollbackCalled = true
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})
	pending := application.PendingAction{Kind: application.ActionRollback, Targets: []string{"cp-1"}}

	effects := application.BuildActionEffects(model, pending)

	require.Len(t, effects, 1)
	effects[0](t.Context(), application.Dependencies{})
	assert.True(t, rollbackCalled)
}

func TestBuildActionEffectsUsesUpgradeWithImageForUpgradeKind(t *testing.T) {
	var gotTarget, gotImage string
	controller := &testkit.FakeNodeController{
		UpgradeFunc: func(_ context.Context, target, image string) error {
			gotTarget, gotImage = target, image
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})
	pending := application.PendingAction{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "ghcr.io/siderolabs/installer:v1.13.3"}

	effects := application.BuildActionEffects(model, pending)

	require.Len(t, effects, 1)
	effects[0](t.Context(), application.Dependencies{})
	assert.Equal(t, "cp-1", gotTarget)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.3", gotImage)
}
```

Append to `internal/application/update_test.go`:

```go
func TestRequestActionCarriesImageForUpgradeKind(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "worker-1", Role: domain.NodeRoleWorker},
	}}}

	got, _ := application.Update(model, application.RequestAction{Kind: application.ActionUpgrade, Targets: []string{"worker-1"}, Image: "ghcr.io/siderolabs/installer:v1.13.3"})

	require.NotNil(t, got.PendingAction)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.3", got.PendingAction.Image)
}

func TestRequestActionComputesQuorumWarningForRollbackKind(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
		{Name: "cp-2", Role: domain.NodeRoleControl},
		{Name: "cp-3", Role: domain.NodeRoleControl},
	}}}
	model.Etcd = application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1"}, {Hostname: "cp-2"}, {Hostname: "cp-3"},
	}}}

	got, _ := application.Update(model, application.RequestAction{Kind: application.ActionRollback, Targets: []string{"cp-1", "cp-2"}})

	require.NotNil(t, got.PendingAction)
	assert.Contains(t, got.PendingAction.Warning, "below quorum", "Rollback must reuse the same quorum warning Reboot/Shutdown use, since it also reboots the node")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/application/... 2>&1 | head -40`
Expected: compile errors — `application.ActionRollback`, `application.ActionUpgrade`, `PendingAction.Image`, `RequestAction.Image`, `FakeNodeController.RollbackFunc`/`UpgradeFunc` undefined (the last two were added in Task 1 already, so only the `application` package symbols should be missing here).

- [ ] **Step 3: Add the action kinds and fields**

In `internal/application/model.go`, extend the `ActionKind` const block:

```go
const (
	ActionReboot   ActionKind = "reboot"
	ActionShutdown ActionKind = "shutdown"
	ActionRollback ActionKind = "rollback"
	ActionUpgrade  ActionKind = "upgrade"
)
```

Add `Image` to `PendingAction` and `RequestAction`:

```go
type PendingAction struct {
	Kind    ActionKind
	Targets []string
	Warning string
	Image   string
}
```

```go
type RequestAction struct {
	Kind    ActionKind
	Targets []string
	Image   string
}
```

- [ ] **Step 4: Split the warning helper and extend actionEffect**

In `internal/application/actions.go`, replace `computeActionWarning` with a thin wrapper plus the extracted quorum math:

```go
func computeActionWarning(nodes []domain.NodeSnapshot, etcd EtcdState, targets []string) string {
	if !targetsIncludeControlPlane(nodes, targets) {
		return ""
	}
	return computeEtcdQuorumWarning(nodes, etcd, targets)
}

func targetsIncludeControlPlane(nodes []domain.NodeSnapshot, targets []string) bool {
	for _, target := range targets {
		for _, node := range nodes {
			if node.Target() != target {
				continue
			}
			if node.Role == domain.NodeRoleControl {
				return true
			}
			break
		}
	}
	return false
}

// computeEtcdQuorumWarning is shared by every action that reboots a
// control-plane node (Reboot, Shutdown, Rollback, Upgrade) and by service
// actions that stop or restart the etcd service directly.
func computeEtcdQuorumWarning(nodes []domain.NodeSnapshot, etcd EtcdState, targets []string) string {
	if etcd.Status != Ready && etcd.Status != Partial {
		return "control-plane node(s); etcd quorum impact unknown (etcd data unavailable)"
	}
	total := len(etcd.Value.Members)
	if total == 0 {
		return "control-plane node(s); etcd membership unknown"
	}
	atRisk := 0
	alreadyUnhealthy := 0
	for _, member := range etcd.Value.Members {
		isTarget := false
		for _, target := range targets {
			if member.Hostname == target {
				isTarget = true
				break
			}
		}
		if isTarget {
			atRisk++
			continue
		}
		if !member.StatusKnown || len(member.Errors) > 0 {
			alreadyUnhealthy++
		}
	}
	remaining := total - atRisk - alreadyUnhealthy
	quorumFloor := total/2 + 1
	if remaining < quorumFloor {
		return fmt.Sprintf("control-plane node(s); would drop etcd to %d/%d — below quorum (need %d)", remaining, total, quorumFloor)
	}
	return "control-plane node(s)"
}
```

Delete the old body of `computeActionWarning` (the function it's replaced by above) — do not leave the old implementation behind.

Replace `actionEffect` and `BuildActionEffects`:

```go
func actionEffect(controller ports.NodeController, pending PendingAction, target string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if controller == nil {
			return ActionFailed{Generation: generation, Target: target, Err: fmt.Errorf("node controller is not configured")}
		}
		var err error
		switch pending.Kind {
		case ActionReboot:
			err = controller.Reboot(ctx, target, ports.RebootDefault)
		case ActionShutdown:
			err = controller.Shutdown(ctx, target, false)
		case ActionRollback:
			err = controller.Rollback(ctx, target)
		case ActionUpgrade:
			err = controller.Upgrade(ctx, target, pending.Image)
		default:
			err = fmt.Errorf("unsupported action %q", pending.Kind)
		}
		if err != nil {
			return ActionFailed{Generation: generation, Target: target, Err: err}
		}
		return ActionSucceeded{Generation: generation, Target: target}
	}
}

func BuildActionEffects(model Model, pending PendingAction) []Effect {
	effects := make([]Effect, 0, len(pending.Targets))
	for _, target := range pending.Targets {
		effects = append(effects, actionEffect(model.nodeController, pending, target, model.Generation))
	}
	return effects
}
```

- [ ] **Step 5: Thread Image through RequestAction handling**

In `internal/application/update.go`, in the `case RequestAction:` block, add `Image` to the constructed `PendingAction`:

```go
	case RequestAction:
		if !model.WritesEnabled || len(message.Targets) == 0 {
			return model, nil
		}
		model.PendingAction = &PendingAction{
			Kind:    message.Kind,
			Targets: append([]string(nil), message.Targets...),
			Warning: computeActionWarning(model.Nodes.Value.Nodes, model.Etcd, message.Targets),
			Image:   message.Image,
		}
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, nil
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/application/model.go internal/application/actions.go internal/application/update.go internal/application/actions_test.go internal/application/update_test.go
git commit -m "feat(application): add ActionRollback and ActionUpgrade action kinds"
```

---

### Task 4: Application layer — service actions (start/stop/restart)

**Files:**
- Modify: `internal/application/model.go`
- Modify: `internal/application/update.go`
- Modify: `internal/application/effects.go`
- Create: `internal/application/service_actions.go`
- Create: `internal/application/service_actions_test.go`
- Modify: `internal/application/update_test.go`

**Interfaces:**
- Consumes: `ports.ServiceController` (Task 2), `computeEtcdQuorumWarning` (Task 3, same package, unexported — direct call).
- Produces: `application.ServiceActionKind` (`ServiceActionStart`/`Stop`/`Restart`), `application.PendingServiceAction`, `application.RequestServiceAction`, `Model.PendingServiceAction *PendingServiceAction`, `application.BuildServiceActionEffect(model Model, pending PendingServiceAction) Effect`. Reuses existing `ActionSucceeded`/`ActionFailed`/`ConfirmPendingAction`/`CancelPendingAction` — no new result-tracking types.

- [ ] **Step 1: Write the failing tests**

Create `internal/application/service_actions_test.go`:

```go
package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildServiceActionEffectCallsRestart(t *testing.T) {
	var gotNode, gotService string
	controller := &testkit.FakeServiceController{
		RestartFunc: func(_ context.Context, node, service string) error {
			gotNode, gotService = node, service
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, ServiceController: controller})
	pending := application.PendingServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "etcd"}

	effect := application.BuildServiceActionEffect(model, pending)
	result := effect(t.Context(), application.Dependencies{})

	assert.Equal(t, "cp-1", gotNode)
	assert.Equal(t, "etcd", gotService)
	assert.Equal(t, application.ActionSucceeded{Generation: model.Generation, Target: "etcd@cp-1"}, result)
}

func TestBuildServiceActionEffectUsesStartAndStop(t *testing.T) {
	var startCalled, stopCalled bool
	controller := &testkit.FakeServiceController{
		StartFunc: func(context.Context, string, string) error {
			startCalled = true
			return nil
		},
		StopFunc: func(context.Context, string, string) error {
			stopCalled = true
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, ServiceController: controller})

	application.BuildServiceActionEffect(model, application.PendingServiceAction{Kind: application.ServiceActionStart, Node: "cp-1", Service: "kubelet"})(t.Context(), application.Dependencies{})
	application.BuildServiceActionEffect(model, application.PendingServiceAction{Kind: application.ServiceActionStop, Node: "cp-1", Service: "kubelet"})(t.Context(), application.Dependencies{})

	assert.True(t, startCalled)
	assert.True(t, stopCalled)
}

func TestBuildServiceActionEffectReturnsActionFailedOnError(t *testing.T) {
	controller := &testkit.FakeServiceController{
		RestartFunc: func(context.Context, string, string) error {
			return errors.New("unreachable")
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, ServiceController: controller})
	pending := application.PendingServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "etcd"}

	result := application.BuildServiceActionEffect(model, pending)(t.Context(), application.Dependencies{})

	require.IsType(t, application.ActionFailed{}, result)
	assert.Equal(t, "etcd@cp-1", result.(application.ActionFailed).Target)
}
```

Append to `internal/application/update_test.go`:

```go
func TestRequestServiceActionSetsPendingServiceAction(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true

	got, effect := application.Update(model, application.RequestServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "kubelet"})

	require.NotNil(t, got.PendingServiceAction)
	assert.Equal(t, application.ServiceActionRestart, got.PendingServiceAction.Kind)
	assert.Equal(t, "cp-1", got.PendingServiceAction.Node)
	assert.Equal(t, "kubelet", got.PendingServiceAction.Service)
	assert.Empty(t, got.PendingServiceAction.Warning, "non-etcd services never carry the quorum warning")
	assert.Nil(t, effect)
}

func TestRequestServiceActionIsInertWhenWritesDisabled(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = false

	got, effect := application.Update(model, application.RequestServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "etcd"})

	assert.Nil(t, got.PendingServiceAction)
	assert.Nil(t, effect)
}

func TestRequestServiceActionWarnsForEtcdRestart(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
		{Name: "cp-2", Role: domain.NodeRoleControl},
		{Name: "cp-3", Role: domain.NodeRoleControl},
	}}}
	model.Etcd = application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1"}, {Hostname: "cp-2"}, {Hostname: "cp-3"},
	}}}

	got, _ := application.Update(model, application.RequestServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "etcd"})

	require.NotNil(t, got.PendingServiceAction)
	assert.Contains(t, got.PendingServiceAction.Warning, "below quorum")
}

func TestRequestServiceActionNoWarningForEtcdStart(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
	}}}

	got, _ := application.Update(model, application.RequestServiceAction{Kind: application.ServiceActionStart, Node: "cp-1", Service: "etcd"})

	require.NotNil(t, got.PendingServiceAction)
	assert.Empty(t, got.PendingServiceAction.Warning, "starting a stopped service never removes quorum capacity")
}

func TestConfirmPendingServiceActionSetsActionTotalToOne(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.PendingServiceAction = &application.PendingServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "kubelet"}

	got, effect := application.Update(model, application.ConfirmPendingAction{})

	assert.Nil(t, got.PendingServiceAction)
	assert.Equal(t, 1, got.ActionTotal)
	assert.Nil(t, effect)
}

func TestCancelPendingActionClearsPendingServiceAction(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.PendingServiceAction = &application.PendingServiceAction{Kind: application.ServiceActionStop, Node: "cp-1", Service: "kubelet"}

	got, effect := application.Update(model, application.CancelPendingAction{})

	assert.Nil(t, got.PendingServiceAction)
	assert.Nil(t, effect)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/application/... 2>&1 | head -40`
Expected: compile errors — `ServiceActionKind`, `PendingServiceAction`, `RequestServiceAction`, `BuildServiceActionEffect`, `SessionOpened.ServiceController`, `testkit.FakeServiceController` (already added in Task 2) undefined in `application` package.

- [ ] **Step 3: Add the model types**

In `internal/application/model.go`, add near `ActionKind`/`PendingAction`:

```go
type ServiceActionKind string

const (
	ServiceActionStart   ServiceActionKind = "start"
	ServiceActionStop    ServiceActionKind = "stop"
	ServiceActionRestart ServiceActionKind = "restart"
)

type PendingServiceAction struct {
	Kind    ServiceActionKind
	Node    string
	Service string
	Warning string
}

type RequestServiceAction struct {
	Kind    ServiceActionKind
	Node    string
	Service string
}

func (RequestServiceAction) applicationMessage() {}
```

Add a field to `Model` (next to `serviceReader ports.ServiceReader`):

```go
	serviceReader     ports.ServiceReader
	serviceController ports.ServiceController
```

Add a field to `Model` next to `PendingAction *PendingAction`:

```go
	PendingAction        *PendingAction
	PendingServiceAction *PendingServiceAction
```

Add a field to `SessionOpened` next to `NodeController ports.NodeController`:

```go
	NodeController    ports.NodeController
	ServiceController ports.ServiceController
```

- [ ] **Step 4: Wire SessionOpened, RequestServiceAction, confirm/cancel, and SelectContext**

In `internal/application/update.go`, in `case SessionOpened:`, add:

```go
		model.serviceController = message.ServiceController
```

Add a new case, placed after `case RequestAction:` and before `case CancelPendingAction:`:

```go
	case RequestServiceAction:
		if !model.WritesEnabled || message.Node == "" || message.Service == "" {
			return model, nil
		}
		warning := ""
		if message.Service == "etcd" && message.Kind != ServiceActionStart {
			warning = computeEtcdQuorumWarning(model.Nodes.Value.Nodes, model.Etcd, []string{message.Node})
		}
		model.PendingServiceAction = &PendingServiceAction{
			Kind:    message.Kind,
			Node:    message.Node,
			Service: message.Service,
			Warning: warning,
		}
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, nil
```

Update `case CancelPendingAction:` and `case ConfirmPendingAction:`:

```go
	case CancelPendingAction:
		model.PendingAction = nil
		model.PendingServiceAction = nil
		return model, nil

	case ConfirmPendingAction:
		if model.PendingAction != nil {
			model.ActionTotal = len(model.PendingAction.Targets)
		} else if model.PendingServiceAction != nil {
			model.ActionTotal = 1
		}
		model.PendingAction = nil
		model.PendingServiceAction = nil
		return model, nil
```

In `case SelectContext:`, add `model.PendingServiceAction = nil` next to the existing `model.PendingAction = nil`.

- [ ] **Step 5: Wire the effect that carries ServiceController through SessionOpened**

In `internal/application/effects.go`, in the function building `SessionOpened` (around the `return SessionOpened{...}` line), add `ServiceController: session.ServiceActions()` to the struct literal:

```go
		return SessionOpened{Generation: generation, Nodes: nodes, NodeController: session.NodeActions(), ServiceController: session.ServiceActions(), Services: session.Services(), Logs: session.ServiceLogs(), Events: session.Events(), Etcd: session.Etcd(), Processes: session.Processes(), Disks: session.Disks(), Network: session.Network(), ResourceKinds: session.ResourceKinds(), Resources: session.Resources(), KubernetesNodes: kubernetesReader}
```

- [ ] **Step 6: Implement BuildServiceActionEffect**

Create `internal/application/service_actions.go`:

```go
package application

import (
	"context"
	"fmt"
)

// BuildServiceActionEffect returns the single Effect for a confirmed service
// action. Unlike node actions, service actions are never bulk — there is at
// most one PendingServiceAction at a time — so this returns one Effect, not
// a slice, and reuses ActionSucceeded/ActionFailed with a synthesized
// "service@node" target, matching the label format services.go/logs.go
// already use for the same node+service pairing.
func BuildServiceActionEffect(model Model, pending PendingServiceAction) Effect {
	target := pending.Service + "@" + pending.Node
	generation := model.Generation
	controller := model.serviceController
	return func(ctx context.Context, _ Dependencies) Message {
		if controller == nil {
			return ActionFailed{Generation: generation, Target: target, Err: fmt.Errorf("service controller is not configured")}
		}
		var err error
		switch pending.Kind {
		case ServiceActionStart:
			err = controller.Start(ctx, pending.Node, pending.Service)
		case ServiceActionStop:
			err = controller.Stop(ctx, pending.Node, pending.Service)
		case ServiceActionRestart:
			err = controller.Restart(ctx, pending.Node, pending.Service)
		default:
			err = fmt.Errorf("unsupported service action %q", pending.Kind)
		}
		if err != nil {
			return ActionFailed{Generation: generation, Target: target, Err: err}
		}
		return ActionSucceeded{Generation: generation, Target: target}
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/application/model.go internal/application/update.go internal/application/effects.go internal/application/service_actions.go internal/application/service_actions_test.go internal/application/update_test.go
git commit -m "feat(application): add service start/stop/restart actions"
```

---

### Task 5: Application layer — Upgrade prompt prefill

**Files:**
- Modify: `internal/application/model.go`
- Modify: `internal/application/update.go`
- Modify: `internal/application/effects.go`
- Modify: `internal/application/update_test.go`

**Interfaces:**
- Consumes: `ports.NodeController.CurrentInstallImage` (Task 1).
- Produces: `application.RequestUpgradePrompt{Target string, Generation uint64}`, `application.UpgradePromptOpened{Target, Image string, Generation uint64}`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/application/update_test.go`:

```go
func TestRequestUpgradePromptFetchesCurrentImage(t *testing.T) {
	controller := &testkit.FakeNodeController{
		CurrentInstallImageFunc: func(_ context.Context, target string) (string, error) {
			assert.Equal(t, "cp-1", target)
			return "ghcr.io/siderolabs/installer:v1.13.2", nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})

	_, effect := application.Update(model, application.RequestUpgradePrompt{Target: "cp-1", Generation: model.Generation})

	require.NotNil(t, effect)
	message := effect(t.Context(), application.Dependencies{})
	assert.Equal(t, application.UpgradePromptOpened{Target: "cp-1", Image: "ghcr.io/siderolabs/installer:v1.13.2", Generation: model.Generation}, message)
}

func TestRequestUpgradePromptOpensBlankOnFetchError(t *testing.T) {
	controller := &testkit.FakeNodeController{
		CurrentInstallImageFunc: func(context.Context, string) (string, error) {
			return "", errors.New("resource not found")
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})

	_, effect := application.Update(model, application.RequestUpgradePrompt{Target: "cp-1", Generation: model.Generation})

	message := effect(t.Context(), application.Dependencies{})
	assert.Equal(t, application.UpgradePromptOpened{Target: "cp-1", Image: "", Generation: model.Generation}, message, "a prefill fetch failure must still open the prompt, just blank")
}
```

`internal/application/update_test.go` already imports `errors` (used by `TestActionFailedAppendsErrorResultForCurrentGeneration`); no new import needed there.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/application/... 2>&1 | head -30`
Expected: compile errors — `application.RequestUpgradePrompt`/`UpgradePromptOpened` undefined.

- [ ] **Step 3: Add the messages**

In `internal/application/model.go`, add near `OpenProcesses`/`ProcessesLoaded`:

```go
type RequestUpgradePrompt struct {
	Target     string
	Generation uint64
}

func (RequestUpgradePrompt) applicationMessage() {}

type UpgradePromptOpened struct {
	Target     string
	Image      string
	Generation uint64
}

func (UpgradePromptOpened) applicationMessage() {}
```

- [ ] **Step 4: Wire the case and the effect**

In `internal/application/update.go`, add a case near `RequestServiceAction`:

```go
	case RequestUpgradePrompt:
		return model, requestUpgradeImage(model.nodeController, message.Target, message.Generation)

	case UpgradePromptOpened:
		return model, nil
```

In `internal/application/effects.go`, add near `loadProcesses`:

```go
func requestUpgradeImage(controller ports.NodeController, target string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if controller == nil {
			return UpgradePromptOpened{Target: target, Generation: generation}
		}
		image, err := controller.CurrentInstallImage(ctx, target)
		if err != nil {
			return UpgradePromptOpened{Target: target, Generation: generation}
		}
		return UpgradePromptOpened{Target: target, Image: image, Generation: generation}
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/application/model.go internal/application/update.go internal/application/effects.go internal/application/update_test.go
git commit -m "feat(application): fetch current install image to prefill the Upgrade prompt"
```

---

### Task 6: TUI — confirm-prompt rendering for Rollback, Upgrade, and service actions

**Files:**
- Modify: `internal/tui/action_prompt.go`
- Modify: `internal/tui/model.go`
- Create: `internal/tui/action_prompt_test.go`

**Interfaces:**
- Consumes: `application.ActionRollback`, `application.ActionUpgrade`, `application.PendingServiceAction` (Tasks 3–4).
- Produces: `renderPendingServiceActionPrompt(pending application.PendingServiceAction) string`; `renderPendingActionPrompt` extended; `model.activePrompt()` extended to check `PendingServiceAction`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/action_prompt_test.go`:

```go
package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/stretchr/testify/assert"
)

func TestRenderPendingActionPromptRollbackVerb(t *testing.T) {
	prompt := renderPendingActionPrompt(application.PendingAction{Kind: application.ActionRollback, Targets: []string{"cp-1"}})

	assert.Equal(t, "Rollback cp-1? (y/n)", prompt)
}

func TestRenderPendingActionPromptUpgradeVerbIncludesImage(t *testing.T) {
	prompt := renderPendingActionPrompt(application.PendingAction{
		Kind:    application.ActionUpgrade,
		Targets: []string{"cp-1"},
		Image:   "ghcr.io/siderolabs/installer:v1.13.3",
	})

	assert.Equal(t, "Upgrade to ghcr.io/siderolabs/installer:v1.13.3 cp-1? (y/n)", prompt)
}

func TestRenderPendingServiceActionPromptFormatsServiceAtNode(t *testing.T) {
	prompt := renderPendingServiceActionPrompt(application.PendingServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "etcd"})

	assert.Equal(t, "Restart etcd@cp-1? (y/n)", prompt)
}

func TestRenderPendingServiceActionPromptIncludesWarning(t *testing.T) {
	prompt := renderPendingServiceActionPrompt(application.PendingServiceAction{
		Kind:    application.ServiceActionStop,
		Node:    "cp-1",
		Service: "etcd",
		Warning: "control-plane node(s); would drop etcd to 1/3 — below quorum (need 2)",
	})

	assert.Contains(t, prompt, "!!")
	assert.Contains(t, prompt, "Stop etcd@cp-1? (y/n)")
}
```

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./internal/tui/... -run TestRenderPending 2>&1 | head -30`
Expected: compile errors — `application.ActionRollback` etc. undefined (only if Task 3/4 weren't already merged; they were, in prior tasks, so the actual failure here is `renderPendingServiceActionPrompt` undefined) and a wrong-value failure for the Rollback/Upgrade verb tests, since `renderPendingActionPrompt` doesn't yet handle those kinds.

- [ ] **Step 3: Extend action_prompt.go**

In `internal/tui/action_prompt.go`, replace the verb resolution in `renderPendingActionPrompt`:

```go
func renderPendingActionPrompt(pending application.PendingAction) string {
	verb := "Reboot"
	switch pending.Kind {
	case application.ActionShutdown:
		verb = "Shutdown"
	case application.ActionRollback:
		verb = "Rollback"
	case application.ActionUpgrade:
		verb = "Upgrade to " + pending.Image
	}
	if pending.Warning != "" {
		warning := truncateWarningTail(pending.Warning, pendingActionWarningBudget)
		return fmt.Sprintf("!! %s — %s %d node(s)? (y/n)", warning, verb, len(pending.Targets))
	}
	return verb + " " + strings.Join(pending.Targets, ", ") + "? (y/n)"
}
```

Add below `renderPendingActionPrompt`:

```go
func renderPendingServiceActionPrompt(pending application.PendingServiceAction) string {
	verb := "Start"
	switch pending.Kind {
	case application.ServiceActionStop:
		verb = "Stop"
	case application.ServiceActionRestart:
		verb = "Restart"
	}
	target := pending.Service + "@" + pending.Node
	if pending.Warning != "" {
		warning := truncateWarningTail(pending.Warning, pendingActionWarningBudget)
		return fmt.Sprintf("!! %s — %s %s? (y/n)", warning, verb, target)
	}
	return verb + " " + target + "? (y/n)"
}
```

- [ ] **Step 4: Wire activePrompt()**

In `internal/tui/model.go`, in `func (m model) activePrompt() string`, add a case right after the `PendingAction` check:

```go
func (m model) activePrompt() string {
	if m.application.PendingAction != nil {
		return renderPendingActionPrompt(*m.application.PendingAction)
	}
	if m.application.PendingServiceAction != nil {
		return renderPendingServiceActionPrompt(*m.application.PendingServiceAction)
	}
	if prompt := m.palette.view(); prompt != "" {
		return prompt
	}
	...
```

(Leave the rest of the function — the filter-prompt branches — unchanged.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/action_prompt.go internal/tui/model.go internal/tui/action_prompt_test.go
git commit -m "feat(tui): render confirm prompts for Rollback, Upgrade, and service actions"
```

---

### Task 7: TUI — Nodes screen Rollback key (B)

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/actions.go`
- Modify: `internal/tui/actions_flow_test.go`
- Modify: `internal/tui/k9s_compat_test.go`

**Interfaces:**
- Consumes: `application.ActionRollback`, `application.RequestAction`, `nodesModel.actionTargets()` (existing).
- Produces: none new — this task only adds key dispatch.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/actions_flow_test.go`:

```go
func TestRollbackKeyWithWritesDisabledIsInert(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))

	updated, _ := root.Update(keyPress('B'))

	assert.Nil(t, updated.(model).application.PendingAction)
}

func TestRollbackKeyWithWritesEnabledOpensConfirmPrompt(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})

	updated, _ := root.Update(keyPress('B'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingAction)
	assert.Equal(t, application.ActionRollback, rootModel.application.PendingAction.Kind)
	assert.Contains(t, rootModel.application.PendingAction.Targets, "cp-1")
}

func TestRollbackKeyHandlesKittyShiftEncoding(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})

	updated, _ := root.Update(shiftKeyPress('B'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingAction, "shift+b (Kitty protocol) must open the confirm prompt, same as legacy \"B\"")
	assert.Equal(t, application.ActionRollback, rootModel.application.PendingAction.Kind)
}
```

Update the `keys` slice for `viewNodes` in `internal/tui/k9s_compat_test.go`:

```go
		{name: "root resources", view: viewNodes, keys: []string{"?", ":", "/", "d", "r", "p", "k", "n", "space", "R", "X", "B"}},
```

`TestK9sCompatibilityActionMatrix` calls `actionHints(test.view, false)` — `writesEnabled: false` — so the existing row already omits `space`/`R`/`X`, and `B` is gated by `writesEnabled` the same way. No change to `k9s_compat_test.go` for this task; leave the row as-is.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run TestRollback 2>&1 | head -30`
Expected: FAIL — `B` does nothing yet, `PendingAction` stays nil.

- [ ] **Step 3: Add the key handler**

In `internal/tui/model.go`, add a new block right after the existing `X` block (after the `if key == "X" && ...` block, before the `space` block):

```go
		if key == "B" && m.application.WritesEnabled && !m.nodes.filtering {
			if targets := m.nodes.actionTargets(); len(targets) > 0 {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionRollback, Targets: targets})
				return m, m.command(effect)
			}
		}
```

- [ ] **Step 4: Add the action hint**

In `internal/tui/actions.go`, in the `viewNodes` branch's `writesEnabled` block, add `B` after `X`:

```go
			if writesEnabled {
				hints = append(hints,
					actionHint{Key: "space", Label: "Mark"},
					actionHint{Key: "R", Label: "Reboot"},
					actionHint{Key: "X", Label: "Shutdown"},
					actionHint{Key: "B", Label: "Rollback"},
				)
			}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/actions.go internal/tui/actions_flow_test.go
git commit -m "feat(tui): add Talos Rollback (B) on the Nodes screen"
```

---

### Task 8: TUI — Upgrade prompt component

**Files:**
- Create: `internal/tui/upgrade_prompt.go`
- Create: `internal/tui/upgrade_prompt_test.go`

**Interfaces:**
- Consumes: `charm.land/bubbles/v2/textinput` (already a dependency, used by `commandModel`).
- Produces: `upgradePromptModel{target string, input textinput.Model}`, `newUpgradePromptModel(target, prefill string) upgradePromptModel`, `(m upgradePromptModel) update(message tea.Msg) (upgradePromptModel, tea.Cmd)`, `(m upgradePromptModel) view() string`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/upgrade_prompt_test.go`:

```go
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestNewUpgradePromptModelPrefillsInput(t *testing.T) {
	m := newUpgradePromptModel("cp-1", "ghcr.io/siderolabs/installer:v1.13.2")

	assert.Equal(t, "cp-1", m.target)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.2", m.input.Value())
}

func TestNewUpgradePromptModelAllowsBlankPrefill(t *testing.T) {
	m := newUpgradePromptModel("cp-1", "")

	assert.Empty(t, m.input.Value())
}

func TestUpgradePromptModelUpdateAppendsTypedText(t *testing.T) {
	m := newUpgradePromptModel("cp-1", "ghcr.io/siderolabs/installer:v1.13.2")

	m, _ = m.update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	assert.Contains(t, m.input.Value(), "x")
}

func TestUpgradePromptModelViewReturnsInputView(t *testing.T) {
	m := newUpgradePromptModel("cp-1", "ghcr.io/siderolabs/installer:v1.13.2")

	assert.Contains(t, m.view(), "ghcr.io/siderolabs/installer:v1.13.2")
}
```

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./internal/tui/... -run TestNewUpgradePromptModel 2>&1 | head -20`
Expected: compile error — `newUpgradePromptModel` undefined.

- [ ] **Step 3: Implement the component**

Create `internal/tui/upgrade_prompt.go`:

```go
package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// upgradePromptModel is the text-input step of the Nodes-screen Upgrade (U)
// flow: prefilled with the target node's current install image, editable,
// and submitted with Enter into the same PendingAction confirm flow Rollback
// and Reboot/Shutdown already use.
type upgradePromptModel struct {
	target string
	input  textinput.Model
}

func newUpgradePromptModel(target, prefill string) upgradePromptModel {
	input := textinput.New()
	input.Prompt = "UPGRADE :"
	input.Placeholder = "installer image"
	input.CharLimit = 256
	input.SetValue(prefill)
	return upgradePromptModel{target: target, input: input}
}

func (m upgradePromptModel) update(message tea.Msg) (upgradePromptModel, tea.Cmd) {
	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

func (m upgradePromptModel) view() string {
	return m.input.View()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/upgrade_prompt.go internal/tui/upgrade_prompt_test.go
git commit -m "feat(tui): add the Upgrade image prompt component"
```

---

### Task 9: TUI — wire the Upgrade key (U) end to end

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/actions.go`
- Modify: `internal/tui/actions_flow_test.go`

**Interfaces:**
- Consumes: `application.RequestUpgradePrompt`, `application.UpgradePromptOpened`, `application.ActionUpgrade` (Tasks 3, 5), `upgradePromptModel` (Task 8), `nodesModel.selected()` (existing).
- Produces: `model.upgradePrompt *upgradePromptModel` field and its full key-routing/open/submit/cancel behavior.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/actions_flow_test.go`:

```go
func TestUpgradeKeyOpensPromptPrefilledWithCurrentImage(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{
		CurrentInstallImageFunc: func(context.Context, string) (string, error) {
			return "ghcr.io/siderolabs/installer:v1.13.2", nil
		},
	})

	updated, cmd := root.Update(keyPress('U'))
	require.NotNil(t, cmd)
	msg := cmd()
	updated, _ = updated.(model).Update(msg)
	rootModel := updated.(model)

	require.NotNil(t, rootModel.upgradePrompt)
	assert.Equal(t, "cp-1", rootModel.upgradePrompt.target)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.2", rootModel.upgradePrompt.input.Value())
}

func TestUpgradePromptEnterOpensPendingActionWithEditedImage(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{
		CurrentInstallImageFunc: func(context.Context, string) (string, error) {
			return "ghcr.io/siderolabs/installer:v1.13.2", nil
		},
	})
	updated, cmd := root.Update(keyPress('U'))
	updated, _ = updated.(model).Update(cmd())
	rootModel := updated.(model)

	rootModel.upgradePrompt.input.SetValue("ghcr.io/siderolabs/installer:v1.13.3")
	updatedAny, _ := rootModel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rootModel = updatedAny.(model)

	require.Nil(t, rootModel.upgradePrompt, "Enter must close the text-input step")
	require.NotNil(t, rootModel.application.PendingAction)
	assert.Equal(t, application.ActionUpgrade, rootModel.application.PendingAction.Kind)
	assert.Equal(t, []string{"cp-1"}, rootModel.application.PendingAction.Targets)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.3", rootModel.application.PendingAction.Image)
}

func TestUpgradePromptEscCancelsWithoutRequestingAction(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{
		CurrentInstallImageFunc: func(context.Context, string) (string, error) {
			return "ghcr.io/siderolabs/installer:v1.13.2", nil
		},
	})
	updated, cmd := root.Update(keyPress('U'))
	updated, _ = updated.(model).Update(cmd())
	rootModel := updated.(model)

	updatedAny, _ := rootModel.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	rootModel = updatedAny.(model)

	assert.Nil(t, rootModel.upgradePrompt)
	assert.Nil(t, rootModel.application.PendingAction)
}

func TestUpgradeKeyWithWritesDisabledIsInert(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))

	_, cmd := root.Update(keyPress('U'))

	assert.Nil(t, cmd)
}
```

Add `"context"` to the import block of `internal/tui/actions_flow_test.go` if not already present (check the file first — `writesEnabledTestModel`'s doc comment already references context usage patterns common in this file, but confirm the literal import list before assuming).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run TestUpgrade 2>&1 | head -40`
Expected: FAIL/compile errors — `model.upgradePrompt` field undefined, `U` key does nothing.

- [ ] **Step 3: Add the field and SelectContext reset**

In `internal/tui/model.go`, add a field to the `model` struct next to `contexts contextsModel`:

```go
	contexts      contextsModel
	upgradePrompt *upgradePromptModel
```

In the `case applicationMessage:` block, extend the existing `SelectContext` special-case:

```go
		if _, ok := message.message.(application.SelectContext); ok {
			// A different Talos context may reuse the same node IDs/hostnames
			// (e.g. cp-1 across clusters); never let a mark or an in-flight
			// upgrade prompt carry across a context switch and silently
			// mistarget the new cluster.
			m.nodes.marked = nil
			m.upgradePrompt = nil
		}
```

- [ ] **Step 4: Route keys to the prompt and handle UpgradePromptOpened**

Still in the `case applicationMessage:` block, just before the final `return m, m.command(effect)` (after the `m.notice = ...` assignment), add:

```go
		var upgradeFocus tea.Cmd
		if opened, ok := message.message.(application.UpgradePromptOpened); ok && opened.Generation == m.application.Generation {
			prompt := newUpgradePromptModel(opened.Target, opened.Image)
			m.upgradePrompt = &prompt
			upgradeFocus = m.upgradePrompt.input.Focus()
		}
		return m, tea.Batch(m.command(effect), upgradeFocus)
```

Replace the plain `return m, m.command(effect)` that used to be the last line of the block with the four lines above.

Now add prompt-routing inside `case tea.KeyPressMsg:`, as another modal-state check alongside the existing `m.contexts.active` and `m.palette.active` checks. In `internal/tui/model.go`, find the `if m.palette.active { switch key { ... } var command tea.Cmd; m.palette, command = m.palette.update(message); return m, command }` block — the one that immediately follows the `if m.contexts.active { ... }` block. Insert the new block immediately after that `if m.palette.active { ... }` block's closing brace, and before the `switch key {` statement that follows it (the switch handling `":"`, `"q"`, `"?"`, `"r"`, etc.). This ordering matters: it must run before that switch and before the per-view key blocks further down (including the Nodes `R`/`X`/`B`/`U` handlers and the Services `S`/`T`/`R` handlers), so that keystrokes are consumed by the open prompt instead of falling through to those bindings.

```go
		if m.upgradePrompt != nil {
			switch key {
			case "esc":
				m.upgradePrompt = nil
				return m, nil
			case "enter":
				target := m.upgradePrompt.target
				image := m.upgradePrompt.input.Value()
				m.upgradePrompt = nil
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionUpgrade, Targets: []string{target}, Image: image})
				return m, m.command(effect)
			}
			var command tea.Cmd
			*m.upgradePrompt, command = m.upgradePrompt.update(message)
			return m, command
		}
```

- [ ] **Step 5: Add the U key handler**

Add a new block right after the `B` block from Task 7 (before the `space` block):

```go
		if key == "U" && m.application.WritesEnabled && !m.nodes.filtering && m.upgradePrompt == nil {
			if node, ok := m.nodes.selected(); ok {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestUpgradePrompt{Target: node.Target(), Generation: m.application.Generation})
				return m, m.command(effect)
			}
		}
```

- [ ] **Step 6: Wire activePrompt() and add the action hint**

In `internal/tui/model.go`, in `activePrompt()`, add a case right after the `PendingServiceAction` check (before `if prompt := m.palette.view(); ...`):

```go
	if m.upgradePrompt != nil {
		return m.upgradePrompt.view()
	}
```

In `internal/tui/actions.go`, add `U` after `B` in the `viewNodes` `writesEnabled` hints:

```go
			if writesEnabled {
				hints = append(hints,
					actionHint{Key: "space", Label: "Mark"},
					actionHint{Key: "R", Label: "Reboot"},
					actionHint{Key: "X", Label: "Shutdown"},
					actionHint{Key: "B", Label: "Rollback"},
					actionHint{Key: "U", Label: "Upgrade"},
				)
			}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/actions.go internal/tui/actions_flow_test.go
git commit -m "feat(tui): add Talos Upgrade (U) on the Nodes screen"
```

---

### Task 10: TUI — Services screen start/stop/restart (S/T/R)

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/actions.go`
- Modify: `internal/tui/actions_flow_test.go`
- Modify: `internal/tui/k9s_compat_test.go`

**Interfaces:**
- Consumes: `application.RequestServiceAction`, `application.ServiceActionStart/Stop/Restart`, `application.BuildServiceActionEffect`, `servicesModel.selected()` (existing).
- Produces: none new — final key-dispatch wiring.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/actions_flow_test.go`:

```go
func writesEnabledServicesTestModel(t *testing.T, controller *testkit.FakeServiceController) model {
	t.Helper()
	appModel, _ := application.NewModel("prod")
	appModel.WritesEnabled = true
	appModel, _ = application.Update(appModel, application.ServicesLoaded{
		Generation: appModel.Generation,
		Services:   domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}}},
	})
	appModel, _ = application.Update(appModel, application.SessionOpened{Generation: appModel.Generation, ServiceController: controller})
	runner := application.NewRunner(application.Dependencies{})
	root := newModel(t.Context(), false, appModel, runner)
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	root.services = root.services.setState(root.application.Services)
	return root
}

func TestServiceRestartKeyWithWritesEnabledOpensConfirmPrompt(t *testing.T) {
	root := writesEnabledServicesTestModel(t, &testkit.FakeServiceController{})

	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingServiceAction)
	assert.Equal(t, application.ServiceActionRestart, rootModel.application.PendingServiceAction.Kind)
	assert.Equal(t, "etcd", rootModel.application.PendingServiceAction.Service)
	assert.Equal(t, "cp-1", rootModel.application.PendingServiceAction.Node)
}

func TestServiceStartKeyWithWritesDisabledIsInert(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	appModel, _ = application.Update(appModel, application.ServicesLoaded{
		Generation: appModel.Generation,
		Services:   domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}}},
	})
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	root.services = root.services.setState(root.application.Services)

	updated, _ := root.Update(keyPress('S'))

	assert.Nil(t, updated.(model).application.PendingServiceAction)
}

func TestServiceStopKeyConfirmedCallsController(t *testing.T) {
	var stopCalled bool
	root := writesEnabledServicesTestModel(t, &testkit.FakeServiceController{
		StopFunc: func(context.Context, string, string) error {
			stopCalled = true
			return nil
		},
	})
	updated, _ := root.Update(keyPress('T'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.application.PendingServiceAction)

	updatedAny, cmd := rootModel.Update(keyPress('y'))
	require.NotNil(t, cmd)
	cmd()

	assert.Nil(t, updatedAny.(model).application.PendingServiceAction)
	assert.True(t, stopCalled)
}

func TestServiceActionOtherKeyCancelsPrompt(t *testing.T) {
	root := writesEnabledServicesTestModel(t, &testkit.FakeServiceController{})
	updated, _ := root.Update(keyPress('S'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.application.PendingServiceAction)

	updatedAny, _ := rootModel.Update(keyPress('n'))

	assert.Nil(t, updatedAny.(model).application.PendingServiceAction)
}
```

Update `internal/tui/k9s_compat_test.go`'s `viewServices` row and the destructive-action doc test:

```go
		{name: "services add Talos logs", view: viewServices, keys: []string{"?", ":", "/", "d", "r", "l"}},
```

This row is unaffected for the same reason as Task 7's Nodes row — `TestK9sCompatibilityActionMatrix` calls `actionHints(test.view, false)`, so the `writesEnabled`-gated `S`/`T`/`R` hints never appear in it. No change to that row.

Update `TestK9sCompatibilityDocumentsReadOnlyTalosDeviations`:

```go
func TestK9sCompatibilityDocumentsReadOnlyTalosDeviations(t *testing.T) {
	serviceHints := renderActionHints(actionHints(viewServices, false))
	assert.Contains(t, serviceHints, "<d> Detail", "Talos services use Enter/d for the same read-only detail")
	for _, destructive := range []string{"Delete", "Kill", "Drain", "Edit"} {
		assert.NotContains(t, serviceHints, destructive, "t9s deliberately omits Kubernetes-pod-style destructive actions; Talos-level service start/stop/restart (S/T/R, WritesEnabled-gated) is a deliberate, separate addition")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run TestService 2>&1 | head -40`
Expected: FAIL — `S`/`T`/`R` do nothing on the Services screen yet, `PendingServiceAction` stays nil.

- [ ] **Step 3: Broaden the confirm-block guard**

In `internal/tui/model.go`, change the guard and the `y`-branch of the existing PendingAction confirm block (originally `if m.application.PendingAction != nil { ... }`) to:

```go
		if m.application.PendingAction != nil || m.application.PendingServiceAction != nil {
			if key == "y" {
				if m.application.PendingAction != nil {
					pending := *m.application.PendingAction
					effects := application.BuildActionEffects(m.application, pending)
					var confirmEffect application.Effect
					m.application, confirmEffect = application.Update(m.application, application.ConfirmPendingAction{})
					m.nodes.marked = nil
					cmds := make([]tea.Cmd, 0, len(effects)+1)
					if cmd := m.command(confirmEffect); cmd != nil {
						cmds = append(cmds, cmd)
					}
					for _, effect := range effects {
						cmds = append(cmds, m.command(effect))
					}
					return m, tea.Batch(cmds...)
				}
				pending := *m.application.PendingServiceAction
				effect := application.BuildServiceActionEffect(m.application, pending)
				var confirmEffect application.Effect
				m.application, confirmEffect = application.Update(m.application, application.ConfirmPendingAction{})
				cmds := make([]tea.Cmd, 0, 2)
				if cmd := m.command(confirmEffect); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if cmd := m.command(effect); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			}
			var effect application.Effect
			m.application, effect = application.Update(m.application, application.CancelPendingAction{})
			return m, m.command(effect)
		}
```

- [ ] **Step 4: Add the service key handlers**

In `internal/tui/model.go`, inside the `if m.views.top().Kind == viewServices { ... }` block, add after the existing `enter`/`d` detail block and before `m.services = m.services.update(message)`:

```go
			if key == "S" && m.application.WritesEnabled && !m.services.filtering {
				if service, ok := m.services.selected(); ok {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RequestServiceAction{Kind: application.ServiceActionStart, Node: service.Node, Service: service.Name})
					return m, m.command(effect)
				}
			}
			if key == "T" && m.application.WritesEnabled && !m.services.filtering {
				if service, ok := m.services.selected(); ok {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RequestServiceAction{Kind: application.ServiceActionStop, Node: service.Node, Service: service.Name})
					return m, m.command(effect)
				}
			}
			if key == "R" && m.application.WritesEnabled && !m.services.filtering {
				if service, ok := m.services.selected(); ok {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RequestServiceAction{Kind: application.ServiceActionRestart, Node: service.Node, Service: service.Name})
					return m, m.command(effect)
				}
			}
```

Note this new `key == "R"` check sits below the top-level refresh `case "r"` switch (lowercase, handled earlier in `Update`) — they're different keys (`R` vs `r`) so there is no collision; `R` reaching this point means refresh's `case "r"` didn't match.

- [ ] **Step 5: Add the action hints**

In `internal/tui/actions.go`, in the `viewServices` branch (`if kind == viewServices { hints = append(hints, actionHint{Key: "l", Label: "Logs"}) }`), extend it:

```go
		if kind == viewServices {
			hints = append(hints, actionHint{Key: "l", Label: "Logs"})
			if writesEnabled {
				hints = append(hints,
					actionHint{Key: "S", Label: "Start"},
					actionHint{Key: "T", Label: "Stop"},
					actionHint{Key: "R", Label: "Restart"},
				)
			}
		} else {
```

(This sits alongside the existing `else { ... p/k/n/R/X ... }` branch for `viewNodes` — keep that branch's contents unchanged, only add the `if writesEnabled` block inside the `viewServices` branch.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/actions.go internal/tui/actions_flow_test.go internal/tui/k9s_compat_test.go
git commit -m "feat(tui): add service start/stop/restart (S/T/R) on the Services screen"
```

---

## Final Verification

- [ ] Run `go build ./...` — no errors.
- [ ] Run `go vet ./...` — no findings.
- [ ] Run `gofmt -l .` — no output.
- [ ] Run `go test ./...` — all packages pass.
- [ ] Manually re-read the diff for Task 9 and Task 10 against the spec's "Error handling" and "Testing" sections to confirm nothing was missed.
