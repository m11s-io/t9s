package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

const maxConfigurationFileBytes = 4 << 20

type directConfigurationPreviewLoaded struct {
	operation  uint64
	generation uint64
	plan       *application.PreparedConfiguration
	basename   string
	err        string
}

type directConfigurationProgress struct {
	operation  uint64
	generation uint64
	result     application.TargetResult
}

type directConfigurationFinished struct {
	operation  uint64
	generation uint64
	result     application.MutationResult
	err        string
}

type directConfigurationExpired struct {
	operation  uint64
	generation uint64
}

type directConfigurationWork struct {
	operation    uint64
	generation   uint64
	ctx          context.Context
	session      *application.Session
	plan         *application.PreparedConfiguration
	confirmation application.Confirmation
	events       chan directConfigurationWorkItem
}

type directConfigurationWorkItem struct {
	result   *application.TargetResult
	finished *directConfigurationFinished
}

func directConfigurationPreviewCommand(session *application.Session, ctx context.Context, operation, generation uint64, path string, targets []string, mode domain.ConfigurationMode) tea.Cmd {
	return func() tea.Msg {
		data, basename, err := readLocalConfiguration(path)
		if err != nil {
			return directConfigurationPreviewLoaded{operation: operation, generation: generation, err: err.Error()}
		}
		plan, err := session.PrepareConfiguration(ctx, application.ConfigurationApplyRequest{Data: data, Targets: targets, Mode: mode})
		clear(data)
		if err != nil {
			return directConfigurationPreviewLoaded{operation: operation, generation: generation, err: safeConfigurationNotice(err)}
		}
		return directConfigurationPreviewLoaded{operation: operation, generation: generation, plan: plan, basename: basename}
	}
}

func readLocalConfiguration(path string) ([]byte, string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, "", errors.New("enter a configuration file path")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", errors.New("unable to read configuration file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", errors.New("configuration path must be a regular file")
	}
	if info.Size() <= 0 {
		return nil, "", errors.New("configuration file is empty")
	}
	if info.Size() > maxConfigurationFileBytes {
		return nil, "", errors.New("configuration file exceeds 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigurationFileBytes+1))
	if err != nil {
		clear(data)
		return nil, "", errors.New("unable to read configuration file")
	}
	if len(data) == 0 {
		return nil, "", errors.New("configuration file is empty")
	}
	if len(data) > maxConfigurationFileBytes {
		clear(data)
		return nil, "", errors.New("configuration file exceeds 4 MiB")
	}
	return data, sanitizeUntrustedText(filepath.Base(path)), nil
}

func safeConfigurationNotice(err error) string {
	if err == nil {
		return ""
	}
	var safe domain.ConfigurationError
	if errors.As(err, &safe) {
		return safe.Error()
	}
	switch {
	case errors.Is(err, application.ErrWritesDisabled):
		return "Configuration apply requires --enable-writes"
	case errors.Is(err, application.ErrSessionClosed):
		return "The Talos session closed before configuration could be prepared"
	case errors.Is(err, application.ErrCapabilityUnavailable):
		return "Configuration apply is unavailable in this session"
	case errors.Is(err, application.ErrAssessmentChanged):
		return "Configuration or target state changed; prepare a new preview"
	case errors.Is(err, application.ErrConfirmationRequired):
		return "Confirmation does not match the prepared configuration"
	case errors.Is(err, application.ErrMutationBusy):
		return "Another mutation is active; try again after it completes"
	default:
		return "Unable to prepare configuration preview"
	}
}

func directConfigurationStartCommand(work directConfigurationWork) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := work.session.ExecuteConfiguration(work.ctx, work.plan, work.confirmation, func(target application.TargetResult) {
				copy := target
				work.events <- directConfigurationWorkItem{result: &copy}
			})
			finished := directConfigurationFinished{operation: work.operation, generation: work.generation, result: result, err: safeConfigurationNotice(err)}
			work.events <- directConfigurationWorkItem{finished: &finished}
		}()
		return directConfigurationAwaitCommand(work)()
	}
}

func directConfigurationAwaitCommand(work directConfigurationWork) tea.Cmd {
	return func() tea.Msg {
		item := <-work.events
		if item.result != nil {
			return directConfigurationProgress{operation: work.operation, generation: work.generation, result: *item.result}
		}
		if item.finished != nil {
			return *item.finished
		}
		return nil
	}
}

func directConfigurationExpiryCommand(operation, generation uint64, expires time.Time) tea.Cmd {
	delay := time.Until(expires)
	if delay < 0 {
		delay = 0
	}
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return directConfigurationExpired{operation: operation, generation: generation}
	})
}
