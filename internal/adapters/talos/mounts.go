package talos

import (
	"context"
	"fmt"
	"sort"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type mountClient interface {
	Mounts(ctx context.Context, node string) (*machineapi.MountsResponse, error)
}

type machineryMountClient struct{ client *talosclient.Client }

func (c machineryMountClient) Mounts(ctx context.Context, node string) (*machineapi.MountsResponse, error) {
	return c.client.Mounts(talosclient.WithNode(ctx, node))
}

type mountReader struct {
	client mountClient
}

func newMountReader(client mountClient) ports.MountReader {
	return &mountReader{client: client}
}

func (r *mountReader) List(ctx context.Context, node string) (domain.MountSet, error) {
	response, err := r.client.Mounts(ctx, node)
	if err != nil {
		return domain.MountSet{}, fmt.Errorf("list mounts: %w", err)
	}
	messages := response.GetMessages()
	if len(messages) == 0 {
		return domain.MountSet{}, fmt.Errorf("mount list from %s returned no messages", node)
	}

	stats := messages[0].GetStats()
	mounts := make([]domain.MountSnapshot, len(stats))
	for index, stat := range stats {
		size := stat.GetSize()
		available := stat.GetAvailable()
		used := size - min(available, size)
		percent := 0.0
		if size > 0 {
			percent = 100 * float64(used) / float64(size)
		}
		mounts[index] = domain.MountSnapshot{
			Filesystem:     stat.GetFilesystem(),
			MountedOn:      stat.GetMountedOn(),
			SizeBytes:      size,
			UsedBytes:      used,
			AvailableBytes: available,
			UsedPercent:    percent,
		}
	}
	sort.SliceStable(mounts, func(i, j int) bool {
		if mounts[i].MountedOn != mounts[j].MountedOn {
			return mounts[i].MountedOn < mounts[j].MountedOn
		}
		return mounts[i].Filesystem < mounts[j].Filesystem
	})

	return domain.MountSet{Mounts: mounts}, nil
}
