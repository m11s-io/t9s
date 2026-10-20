package talos

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/siderolabs/talos/pkg/machinery/client/config"
)

func (b *kubernetesUpgradeBackend) RunControlPlane(ctx context.Context, plan domain.KubernetesUpgradePlan, progress func(domain.KubernetesUpgradeEvent)) error {
	if b == nil || b.talosConfig == nil || b.talosConfig.Contexts[b.contextName] == nil {
		return errors.New("Kubernetes upgrade capability is unavailable")
	}
	identity, err := inspectTalosCLI(ctx)
	if err != nil || identity != plan.CLI {
		return errors.New("talosctl identity changed after preview")
	}
	if plan.SkipControlPlane {
		return nil
	}
	configDir, configPath, err := b.privateTalosConfig()
	if err != nil {
		return errors.New("unable to prepare private Talos CLI credentials")
	}
	defer os.RemoveAll(configDir)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	command := exec.Command(plan.CLI.Path,
		"--talosconfig", configPath,
		"--context", b.contextName,
		"--endpoints", plan.Gateway,
		"--nodes", plan.Gateway,
		"upgrade-k8s",
		"--from", plan.SourceVersion,
		"--to", plan.TargetVersion,
		"--upgrade-kubelet=false",
		"--pre-pull-images=true",
	)
	configureUpgradeProcess(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return errors.New("unable to open talosctl output")
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return errors.New("unable to open talosctl error output")
	}
	if err := command.Start(); err != nil {
		return errors.New("unable to start talosctl Kubernetes upgrade")
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); consumeUpgradeOutput(stdout, progress) }()
	go func() { defer readers.Done(); consumeUpgradeOutput(stderr, progress) }()
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-waited:
	case <-ctx.Done():
		_ = terminateUpgradeProcess(command)
		timer := time.NewTimer(5 * time.Second)
		terminated := false
		select {
		case waitErr = <-waited:
			terminated = true
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		_ = killUpgradeProcess(command)
		if !terminated {
			waitErr = <-waited
		}
		readers.Wait()
		return errors.New("talosctl Kubernetes upgrade stopped; cluster state may be partial")
	}
	readers.Wait()
	if ctx.Err() != nil {
		return errors.New("talosctl Kubernetes upgrade stopped; cluster state may be partial")
	}
	if waitErr != nil {
		return errors.New("talosctl Kubernetes upgrade failed; cluster state may be partial")
	}
	return nil
}

func (b *kubernetesUpgradeBackend) privateTalosConfig() (string, string, error) {
	selected := b.talosConfig.Contexts[b.contextName]
	if selected == nil {
		return "", "", errors.New("selected Talos context is missing")
	}
	contextCopy := *selected
	contextCopy.Endpoints = append([]string(nil), selected.Endpoints...)
	contextCopy.Nodes = append([]string(nil), selected.Nodes...)
	contextCopy.Auth = selected.Auth
	if selected.Auth.Basic != nil {
		basic := *selected.Auth.Basic
		contextCopy.Auth.Basic = &basic
	}
	if selected.Auth.SideroV1 != nil {
		sidero := *selected.Auth.SideroV1
		contextCopy.Auth.SideroV1 = &sidero
	}
	privateConfig := &config.Config{Context: b.contextName, Contexts: map[string]*config.Context{b.contextName: &contextCopy}}
	contents, err := privateConfig.Bytes()
	if err != nil {
		return "", "", err
	}
	directory, err := os.MkdirTemp("", "t9s-upgrade-")
	if err != nil {
		clear(contents)
		return "", "", err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		clear(contents)
		os.RemoveAll(directory)
		return "", "", err
	}
	path := filepath.Join(directory, "talosconfig")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		clear(contents)
		os.RemoveAll(directory)
		return "", "", err
	}
	_, writeErr := file.Write(contents)
	clear(contents)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.RemoveAll(directory)
		return "", "", errors.New("unable to write private Talos config")
	}
	return directory, path, nil
}

func consumeUpgradeOutput(reader io.Reader, progress func(domain.KubernetesUpgradeEvent)) {
	buffered := bufio.NewReaderSize(reader, 4096)
	var line strings.Builder
	discard := false
	for {
		part, err := buffered.ReadSlice('\n')
		if !discard {
			if line.Len()+len(part) > 64*1024 {
				line.Reset()
				discard = true
			} else {
				line.Write(part)
			}
		}
		if err == nil {
			if !discard {
				emitUpgradeStage(line.String(), progress)
			}
			line.Reset()
			discard = false
			continue
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err == io.EOF {
			if !discard && line.Len() > 0 {
				emitUpgradeStage(line.String(), progress)
			}
			return
		}
		return
	}
}

func emitUpgradeStage(line string, progress func(domain.KubernetesUpgradeEvent)) {
	if progress == nil {
		return
	}
	line = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, line)
	line = strings.ToLower(line)
	stage, ok := domain.KubernetesUpgradeStage(""), false
	switch {
	case strings.Contains(line, "pre-pulling") || strings.Contains(line, "pre-pull"):
		stage, ok = domain.KubernetesUpgradePrePull, true
	case strings.Contains(line, "kube-apiserver") || strings.Contains(line, "controller-manager") || strings.Contains(line, "scheduler"):
		stage, ok = domain.KubernetesUpgradeControlPlane, true
	case strings.Contains(line, "kube-proxy"):
		stage, ok = domain.KubernetesUpgradeKubeProxy, true
	case strings.Contains(line, "manifest"):
		stage, ok = domain.KubernetesUpgradeManifests, true
	}
	if ok {
		progress(domain.KubernetesUpgradeEvent{Stage: stage, Status: "upstream-progress"})
	}
}

func digestPrivateTalosConfig(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
