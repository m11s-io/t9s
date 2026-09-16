package talos

import (
	"context"
	"fmt"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type memoryClient interface {
	Memory(ctx context.Context, node string) (*machineapi.MemoryResponse, error)
}

type machineryMemoryClient struct{ client *talosclient.Client }

func (c machineryMemoryClient) Memory(ctx context.Context, node string) (*machineapi.MemoryResponse, error) {
	return c.client.Memory(talosclient.WithNode(ctx, node))
}

type memoryReader struct {
	client memoryClient
}

func newMemoryReader(client memoryClient) ports.MemoryReader {
	return &memoryReader{client: client}
}

func (r *memoryReader) List(ctx context.Context, node string) (domain.MemorySnapshot, error) {
	response, err := r.client.Memory(ctx, node)
	if err != nil {
		return domain.MemorySnapshot{}, fmt.Errorf("read memory: %w", err)
	}
	messages := response.GetMessages()
	if len(messages) == 0 {
		return domain.MemorySnapshot{}, fmt.Errorf("memory info from %s returned no messages", node)
	}

	info := messages[0].GetMeminfo()
	if info == nil {
		return domain.MemorySnapshot{}, nil
	}

	// procfs MemInfo fields (and therefore the machine API Meminfo) are raw
	// kB, not bytes; normalize once here so the domain is byte-consistent.
	const kb = 1024
	total := info.GetMemtotal() * kb
	available := info.GetMemavailable() * kb
	used := uint64(0)
	if available < total {
		used = total - available
	}

	return domain.MemorySnapshot{
		TotalBytes:     total,
		FreeBytes:      info.GetMemfree() * kb,
		AvailableBytes: available,
		UsedBytes:      used,
		BuffersBytes:   info.GetBuffers() * kb,
		CachedBytes:    info.GetCached() * kb,
		SwapTotalBytes: info.GetSwaptotal() * kb,
		SwapFreeBytes:  info.GetSwapfree() * kb,
		DirtyBytes:     info.GetDirty() * kb,
		SlabBytes:      info.GetSlab() * kb,
	}, nil
}
