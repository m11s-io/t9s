package talos

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	storageapi "github.com/siderolabs/talos/pkg/machinery/api/storage"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type diskClient interface {
	Disks(ctx context.Context, node string) (*storageapi.DisksResponse, error)
}

type machineryDiskClient struct{ client *talosclient.Client }

func (c machineryDiskClient) Disks(ctx context.Context, node string) (*storageapi.DisksResponse, error) {
	return c.client.Disks(talosclient.WithNode(ctx, node))
}

type diskReader struct {
	client diskClient
}

func newDiskReader(client diskClient) ports.DiskReader {
	return &diskReader{client: client}
}

func (r *diskReader) List(ctx context.Context, node string) (domain.DiskSet, error) {
	response, err := r.client.Disks(ctx, node)
	if err != nil {
		return domain.DiskSet{}, fmt.Errorf("list disks: %w", err)
	}
	messages := response.GetMessages()
	if len(messages) == 0 {
		return domain.DiskSet{}, fmt.Errorf("disk list from %s returned no messages", node)
	}
	infos := messages[0].GetDisks()
	disks := make([]domain.DiskSnapshot, len(infos))
	for index, info := range infos {
		disks[index] = domain.DiskSnapshot{
			DeviceName: info.GetDeviceName(),
			Model:      info.GetModel(),
			Serial:     info.GetSerial(),
			Type:       strings.ToLower(storageapi.Disk_DiskType_name[int32(info.GetType())]),
			SizeBytes:  info.GetSize(),
			BusPath:    info.GetBusPath(),
			SystemDisk: info.GetSystemDisk(),
			ReadOnly:   info.GetReadonly(),
		}
	}
	sort.SliceStable(disks, func(i, j int) bool {
		return disks[i].DeviceName < disks[j].DeviceName
	})

	return domain.DiskSet{Disks: disks}, nil
}
