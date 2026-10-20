package application

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

const unaryReadTimeout = 15 * time.Second

const (
	diskUsageTimeout = 30 * time.Second
	maxDiskUsageRows = 5000
	imageListTimeout = 30 * time.Second
	maxImageRows     = 5000
)

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

func (s *Session) OpenDiskUsage(ctx context.Context, request domain.DiskUsageRequest) (ports.DiskUsageStream, error) {
	if request.Node == "" || !path.IsAbs(request.Path) || strings.ContainsFunc(request.Path, unicode.IsControl) {
		return nil, fmt.Errorf("disk usage requires a node and an absolute path")
	}
	request.Path = path.Clean(request.Path)
	operationCtx, cancel, err := s.operationContext(ctx, diskUsageTimeout)
	if err != nil {
		return nil, err
	}
	if s.sdkSession == nil || s.sdkSession.DiskUsage() == nil {
		cancel()
		return nil, ErrCapabilityUnavailable
	}
	reader := s.sdkSession.DiskUsage()
	stream, err := openLifecycleStream(s, operationCtx,
		func(ctx context.Context) (ports.DiskUsageStream, error) { return reader.Open(ctx, request) },
		func(stream ports.DiskUsageStream) func(context.Context) (domain.DiskUsageBatch, error) {
			return stream.Next
		},
		func(stream ports.DiskUsageStream) error { return stream.Close() },
		func(batch domain.DiskUsageBatch, err error) bool { return err != nil || batch.EOF || batch.Err != "" },
	)
	if err != nil {
		cancel()
		return nil, err
	}
	return &boundedDiskUsageStream{stream: stream, ctx: operationCtx, cancel: cancel}, nil
}

func (s *Session) Containers(ctx context.Context, request domain.ContainerRequest) (domain.ContainerSet, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.ContainerSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Containers() == nil {
		return domain.ContainerSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Containers().List(ctx, request)
}

func (s *Session) OpenImages(ctx context.Context, request domain.ImageRequest) (ports.ImageStream, error) {
	if request.Node == "" {
		return nil, fmt.Errorf("images require a node")
	}
	operationCtx, cancel, err := s.operationContext(ctx, imageListTimeout)
	if err != nil {
		return nil, err
	}
	if s.sdkSession == nil || s.sdkSession.Containers() == nil {
		cancel()
		return nil, ErrCapabilityUnavailable
	}
	reader := s.sdkSession.Containers()
	stream, err := openLifecycleStream(s, operationCtx,
		func(ctx context.Context) (ports.ImageStream, error) { return reader.OpenImages(ctx, request) },
		func(stream ports.ImageStream) func(context.Context) (domain.ImageBatch, error) { return stream.Next },
		func(stream ports.ImageStream) error { return stream.Close() },
		func(batch domain.ImageBatch, err error) bool { return err != nil || batch.EOF },
	)
	if err != nil {
		cancel()
		return nil, err
	}
	return &boundedImageStream{stream: stream, ctx: operationCtx, cancel: cancel}, nil
}

type boundedDiskUsageStream struct {
	stream ports.DiskUsageStream
	ctx    context.Context
	cancel context.CancelFunc
	rows   int
}

func (s *boundedDiskUsageStream) Next(ctx context.Context) (domain.DiskUsageBatch, error) {
	batch, err := s.stream.Next(ctx)
	if err != nil && errors.Is(err, context.DeadlineExceeded) && s.ctx.Err() == context.DeadlineExceeded && (ctx == nil || ctx.Err() == nil) {
		batch.EOF = true
		batch.Truncated = true
		batch.Err = "directory scan reached the 30 second limit"
		return batch, nil
	}
	if err != nil {
		return batch, err
	}
	remaining := maxDiskUsageRows - s.rows
	if len(batch.Rows) > remaining {
		batch.Rows = batch.Rows[:remaining]
		batch.Truncated = true
	}
	s.rows += len(batch.Rows)
	if s.rows >= maxDiskUsageRows && !batch.EOF {
		batch.EOF = true
		batch.Truncated = true
		if batch.Err == "" {
			batch.Err = "showing the first 5,000 entries"
		}
		_ = s.Close()
	}
	return batch, nil
}

func (s *boundedDiskUsageStream) Close() error {
	err := s.stream.Close()
	s.cancel()
	return err
}

type boundedImageStream struct {
	stream ports.ImageStream
	ctx    context.Context
	cancel context.CancelFunc
	rows   int
}

func (s *boundedImageStream) Next(ctx context.Context) (domain.ImageBatch, error) {
	batch, err := s.stream.Next(ctx)
	if err != nil && errors.Is(err, context.DeadlineExceeded) && s.ctx.Err() == context.DeadlineExceeded && (ctx == nil || ctx.Err() == nil) {
		batch.EOF = true
		batch.Truncated = true
		batch.Err = "image list reached the 30 second limit"
		return batch, nil
	}
	if err != nil {
		return batch, err
	}
	remaining := maxImageRows - s.rows
	if len(batch.Images) > remaining {
		batch.Images = batch.Images[:remaining]
		batch.Truncated = true
	}
	s.rows += len(batch.Images)
	if s.rows >= maxImageRows && !batch.EOF {
		batch.EOF = true
		batch.Truncated = true
		if batch.Err == "" {
			batch.Err = "showing the first 5,000 images"
		}
		_ = s.Close()
	}
	return batch, nil
}

func (s *boundedImageStream) Close() error {
	err := s.stream.Close()
	s.cancel()
	return err
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
