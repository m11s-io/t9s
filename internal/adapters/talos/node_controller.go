package talos

import (
	"context"
	"fmt"
	"github.com/blang/semver/v4"
	"io"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
	runtimeresource "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

type lifecycleUpgradeClient interface {
	lifecycleUpgrade(context.Context, string, func(ports.UpgradeEvent)) error
}

type versionedUpgradeClient interface {
	upgradeVersion(context.Context) (string, error)
}

type nodeControlClient interface {
	Reboot(ctx context.Context, opts ...talosclient.RebootMode) error
	Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error
	Rollback(ctx context.Context) error
	Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error
	CurrentInstallImage(ctx context.Context) (string, error)
}

type machineryNodeControlClient struct{ client *talosclient.Client }

func (c machineryNodeControlClient) Reboot(ctx context.Context, opts ...talosclient.RebootMode) error {
	return c.client.Reboot(ctx, opts...)
}

func (c machineryNodeControlClient) Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error {
	return c.client.Shutdown(ctx, opts...)
}

func (c machineryNodeControlClient) Rollback(ctx context.Context) error {
	return c.client.Rollback(ctx)
}

func (c machineryNodeControlClient) Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error {
	_, err := c.client.UpgradeWithOptions(ctx, opts...)
	return err
}

// deriveUpgradeImage combines a declared install image's registry/repository
// prefix with the currently-running Talos version tag. Talos does not
// rewrite the declared machine config's install.image field after an
// out-of-band `talosctl upgrade`, so the declared value alone can be stale —
// prefilling the Upgrade prompt with it verbatim risks a silent downgrade if
// an operator accepts the prefill without editing it. Digest references
// (containing "@") are left untouched, since there is no tag to replace.
func deriveUpgradeImage(declaredImage, runningTag string) string {
	if declaredImage == "" || runningTag == "" || strings.Contains(declaredImage, "@") {
		return declaredImage
	}
	lastSlash := strings.LastIndex(declaredImage, "/")
	lastSegment := declaredImage[lastSlash+1:]
	if colon := strings.LastIndex(lastSegment, ":"); colon >= 0 {
		return declaredImage[:lastSlash+1+colon] + ":" + runningTag
	}
	return declaredImage + ":" + runningTag
}

func (c machineryNodeControlClient) CurrentInstallImage(ctx context.Context) (string, error) {
	cfg, err := safe.StateGet[*talosconfig.MachineConfig](
		ctx, c.client.COSI,
		resource.NewMetadata(talosconfig.NamespaceName, talosconfig.MachineConfigType, talosconfig.ActiveID, resource.VersionUndefined),
	)
	if err != nil {
		return "", err
	}
	declaredImage := cfg.Provider().Machine().Install().Image()
	var schematicFactory, schematicFlavor, schematicID, schematicAuthor string
	if extensions, listErr := safe.StateListAll[*runtimeresource.ExtensionStatus](ctx, c.client.COSI); listErr == nil {
		for extension := range extensions.All() {
			if extension.TypedSpec().Metadata.Name == "schematic" {
				schematicID = extension.TypedSpec().Metadata.Version
				schematicAuthor = extension.TypedSpec().Metadata.Author
				break
			}
		}
		if schematicID != "" {
			schematicFlavor, schematicFactory = parseSchematicAuthor(schematicAuthor)
		}
	}

	versionResponse, err := c.client.Version(ctx)
	if err != nil {
		// The declared image is still a valid, if possibly stale, prefill —
		// don't fail the whole prompt just because the live version query
		// failed.
		return declaredImage, nil
	}
	for _, message := range versionResponse.GetMessages() {
		if version := message.GetVersion(); version != nil {
			if image := deriveSchematicInstallerImage(schematicFactory, schematicFlavor, schematicID, version.GetTag()); image != "" {
				return image, nil
			}
			return deriveUpgradeImage(declaredImage, version.GetTag()), nil
		}
	}
	return declaredImage, nil
}

type nodeController struct{ client nodeControlClient }
type upgradeStream struct {
	results chan ports.UpgradeResult
	cancel  context.CancelFunc
}

func (s *upgradeStream) Results() <-chan ports.UpgradeResult { return s.results }
func (s *upgradeStream) Cancel()                             { s.cancel() }

func newNodeController(client nodeControlClient) ports.NodeController {
	return &nodeController{client: client}
}

func (c *nodeController) Reboot(ctx context.Context, target string, mode ports.RebootMode) error {
	var opts []talosclient.RebootMode
	if mode == ports.RebootPowercycle {
		opts = append(opts, talosclient.WithPowerCycle)
	}
	if err := c.client.Reboot(talosclient.WithNode(ctx, target), opts...); err != nil {
		return fmt.Errorf("reboot %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) Shutdown(ctx context.Context, target string, force bool) error {
	var opts []talosclient.ShutdownOption
	if force {
		opts = append(opts, talosclient.WithShutdownForce(true))
	}
	if err := c.client.Shutdown(talosclient.WithNode(ctx, target), opts...); err != nil {
		return fmt.Errorf("shutdown %s: %w", target, err)
	}
	return nil
}

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

func (c *nodeController) UpgradeStream(ctx context.Context, target, image string) ports.UpgradeStream {
	streamCtx, cancel := context.WithCancel(ctx)
	stream := &upgradeStream{results: make(chan ports.UpgradeResult, 1), cancel: cancel}
	go func() {
		defer close(stream.results)
		defer cancel()
		stream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeChecking, Message: "checking Talos upgrade API"}}
		err := error(nil)
		if versioned, ok := c.client.(versionedUpgradeClient); ok {
			if version, versionErr := versioned.upgradeVersion(streamCtx); versionErr != nil {
				stream.results <- ports.UpgradeResult{Err: versionErr, Done: true}
				return
			} else if !supportsLifecycleUpgradeAPI(version) {
				err := c.Upgrade(streamCtx, target, image)
				if err == nil {
					stream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeComplete, Message: "legacy upgrade accepted"}, Done: true}
				} else {
					stream.results <- ports.UpgradeResult{Err: err, Done: true}
				}
				return
			}
		}
		if lifecycle, ok := c.client.(lifecycleUpgradeClient); ok {
			err = lifecycle.lifecycleUpgrade(streamCtx, image, func(event ports.UpgradeEvent) { stream.results <- ports.UpgradeResult{Event: &event} })
		} else {
			err = c.Upgrade(streamCtx, target, image)
		}
		if err == nil {
			stream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeComplete, Message: "upgrade complete"}, Done: true}
		} else {
			stream.results <- ports.UpgradeResult{Err: err, Done: true}
		}
	}()
	return stream
}

func (c *nodeController) CurrentInstallImage(ctx context.Context, target string) (string, error) {
	image, err := c.client.CurrentInstallImage(talosclient.WithNode(ctx, target))
	if err != nil {
		return "", fmt.Errorf("current install image %s: %w", target, err)
	}
	return image, nil
}
func deriveSchematicInstallerImage(factory, flavor, schematic, tag string) string {
	if factory == "" || flavor == "" || schematic == "" || tag == "" {
		return ""
	}
	return factory + "/" + flavor + "-installer/" + schematic + ":" + tag
}
func parseSchematicAuthor(author string) (flavor, factory string) {
	idx := strings.LastIndex(author, " (")
	if idx < 0 {
		return author, "factory.talos.dev"
	}
	return author[:idx], strings.TrimSuffix(author[idx+2:], ")")
}
func supportsLifecycleUpgradeAPI(raw string) bool {
	version, err := semver.Parse(strings.TrimPrefix(raw, "v"))
	if err != nil {
		return false
	}
	min, _ := semver.Parse("1.13.0-alpha.2")
	max, _ := semver.Parse("2.0.0")
	return version.GT(min) && version.LT(max)
}
func (c machineryNodeControlClient) lifecycleUpgrade(ctx context.Context, image string, progress func(ports.UpgradeEvent)) error {
	stream, err := c.client.LifecycleClient.Upgrade(ctx, &machineapi.LifecycleServiceUpgradeRequest{Source: &machineapi.InstallArtifactsSource{ImageName: image}})
	if err != nil {
		return err
	}
	progress(ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: "installing Talos image"})
	for {
		response, recvErr := stream.Recv()
		if recvErr == io.EOF {
			return fmt.Errorf("lifecycle upgrade ended without exit code")
		}
		if recvErr != nil {
			return recvErr
		}
		if response.GetProgress() == nil {
			continue
		}
		switch value := response.GetProgress().GetResponse().(type) {
		case *machineapi.LifecycleServiceInstallProgress_Message:
			progress(ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: value.Message})
		case *machineapi.LifecycleServiceInstallProgress_ExitCode:
			if value.ExitCode != 0 {
				return fmt.Errorf("lifecycle upgrade failed with exit code %d", value.ExitCode)
			}
			return nil
		}
	}
}
func (c machineryNodeControlClient) upgradeVersion(ctx context.Context) (string, error) {
	response, err := c.client.Version(ctx)
	if err != nil {
		return "", err
	}
	for _, message := range response.GetMessages() {
		if version := message.GetVersion(); version != nil {
			return version.GetTag(), nil
		}
	}
	return "", fmt.Errorf("Talos version response contained no version")
}
