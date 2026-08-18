package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/m11s-io/t9s/internal/adapters/kubernetes"
	"github.com/m11s-io/t9s/internal/adapters/talos"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/config"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/tui"
	"github.com/m11s-io/t9s/internal/version"
	"k8s.io/klog/v2"
)

// configureTUILogging prevents libraries that use klog (notably client-go)
// from writing directly to the terminal while Bubble Tea owns it. T9s own
// CLI errors still use the caller-provided error writer.
func configureTUILogging() {
	klog.LogToStderr(false)
	klog.SetOutput(io.Discard)
}

func resolveKubeContext(ctx context.Context, catalog ports.ContextCatalog, associations config.Associations, kubeContext string) (talosContext string, openPicker bool, err error) {
	if mapped, ok := associations.TalosContextFor(kubeContext); ok {
		return mapped, false, nil
	}

	contexts, err := catalog.List(ctx)
	if err != nil {
		return "", false, err
	}
	for _, clusterContext := range contexts {
		if clusterContext.Name == kubeContext {
			return kubeContext, false, nil
		}
	}

	return "", true, nil
}

func Run(ctx context.Context, args []string, input io.Reader, output io.Writer, errorOutput io.Writer) int {
	configureTUILogging()

	var talosconfigs []string
	var contextOverride string
	var node string
	var kubeContext string
	var enableWrites bool
	started := false

	command := &cobra.Command{
		Use:           "t9s",
		Short:         "Inspect Talos clusters from the terminal",
		Version:       version.String(),
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(*cobra.Command, []string) error {
			started = true
			catalog := talos.NewConfigCatalog(talosconfigs...)

			openPicker := false
			if contextOverride == "" && kubeContext != "" {
				associations, loadErr := config.Load(config.DefaultPath())
				if loadErr != nil {
					_, _ = fmt.Fprintln(errorOutput, loadErr)
				}
				resolved, needsPicker, resolveErr := resolveKubeContext(ctx, catalog, associations, kubeContext)
				if resolveErr != nil {
					return resolveErr
				}
				contextOverride = resolved
				openPicker = needsPicker
			}

			applicationModel, _ := application.NewModel(contextOverride)
			applicationModel.NodeFocus = node
			applicationModel.OpenContextPicker = openPicker
			// T9S_ENABLE_WRITES is parsed as a standard Go bool string
			// (accepts 1, t, T, TRUE, true, True and their false
			// counterparts per strconv.ParseBool); unset, empty, or any
			// unparseable value is treated as disabled — a safety gate must
			// fail closed, not turn on for an arbitrary non-empty string
			// like "false" or "0".
			envEnabled, _ := strconv.ParseBool(os.Getenv("T9S_ENABLE_WRITES"))
			applicationModel.WritesEnabled = enableWrites || envEnabled
			runner := application.NewRunner(application.Dependencies{
				ContextCatalog:     catalog,
				SessionFactory:     talos.NewSessionFactory(talosconfigs...),
				KubernetesResolver: kubernetes.NewResolver(),
			})
			terminalModel, cleanup := tui.NewWithCleanup(ctx, applicationModel, runner)
			program := tea.NewProgram(
				terminalModel,
				tea.WithInput(input),
				tea.WithOutput(output),
				tea.WithWindowSize(120, 40),
			)

			_, programErr := program.Run()
			cleanup()
			return programErr
		},
	}
	command.Flags().StringArrayVar(&talosconfigs, "talosconfig", nil, "path to talosconfig; repeat for multiple files; empty uses TALOSCONFIGS or Talos defaults")
	command.Flags().StringVar(&contextOverride, "context", "", "Talos context override")
	command.Flags().StringVar(&node, "node", "", "initial node focus hint")
	command.Flags().StringVar(&kubeContext, "kube-context", "", "Kubernetes context hint for k9s-launcher association")
	command.Flags().BoolVar(&enableWrites, "enable-writes", false, "enable node lifecycle actions (reboot, shutdown); read-only by default")
	command.SetArgs(args)
	command.SetIn(input)
	command.SetOut(output)
	command.SetErr(errorOutput)

	if err := command.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(errorOutput, err)
		if !started {
			return 2
		}

		return 1
	}

	return 0
}
