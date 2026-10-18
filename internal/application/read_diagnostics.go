package application

import (
	"context"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

const unaryReadTimeout = 15 * time.Second

func (s *Session) Processes(ctx context.Context, node string) (domain.ProcessSet, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.ProcessSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Processes() == nil {
		return domain.ProcessSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Processes().List(ctx, node)
}

func (s *Session) Disks(ctx context.Context, node string) (domain.DiskSet, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.DiskSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Disks() == nil {
		return domain.DiskSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Disks().List(ctx, node)
}

func (s *Session) Network(ctx context.Context, node string) (domain.NetworkSet, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.NetworkSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Network() == nil {
		return domain.NetworkSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Network().List(ctx, node)
}

func (s *Session) Sockets(ctx context.Context, node string) (domain.SocketSet, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.SocketSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Netstat() == nil {
		return domain.SocketSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Netstat().List(ctx, node)
}

func (s *Session) Mounts(ctx context.Context, node string) (domain.MountSet, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.MountSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Mounts() == nil {
		return domain.MountSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Mounts().List(ctx, node)
}

func (s *Session) Memory(ctx context.Context, node string) (domain.MemorySnapshot, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.MemorySnapshot{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Memory() == nil {
		return domain.MemorySnapshot{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Memory().List(ctx, node)
}

func (s *Session) CPU(ctx context.Context, node string) (domain.CPUSnapshot, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.CPUSnapshot{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.CPU() == nil {
		return domain.CPUSnapshot{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.CPU().Sample(ctx, node)
}

func (s *Session) Time(ctx context.Context, node string) (domain.TimeSnapshot, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.TimeSnapshot{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Time() == nil {
		return domain.TimeSnapshot{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Time().Read(ctx, node)
}

func (s *Session) CheckTime(ctx context.Context, node, server string) (domain.TimeCheckResult, error) {
	server, err := ValidateTimeServer(server)
	if err != nil {
		return domain.TimeCheckResult{}, err
	}
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.TimeCheckResult{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Time() == nil {
		return domain.TimeCheckResult{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Time().Check(ctx, node, server)
}

func (s *Session) CurrentInstallImage(ctx context.Context, node string) (string, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return "", err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.NodeActions() == nil {
		return "", ErrCapabilityUnavailable
	}
	return s.sdkSession.NodeActions().CurrentInstallImage(ctx, node)
}
