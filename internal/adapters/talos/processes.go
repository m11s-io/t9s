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

type processClient interface {
	Processes(ctx context.Context, node string) (*machineapi.ProcessesResponse, error)
}

type machineryProcessClient struct{ client *talosclient.Client }

func (c machineryProcessClient) Processes(ctx context.Context, node string) (*machineapi.ProcessesResponse, error) {
	return c.client.Processes(talosclient.WithNode(ctx, node))
}

type processReader struct {
	client processClient
}

func newProcessReader(client processClient) ports.ProcessReader {
	return &processReader{client: client}
}

func (r *processReader) List(ctx context.Context, node string) (domain.ProcessSet, error) {
	response, err := r.client.Processes(ctx, node)
	if err != nil {
		return domain.ProcessSet{}, fmt.Errorf("list processes: %w", err)
	}
	messages := response.GetMessages()
	if len(messages) == 0 {
		return domain.ProcessSet{}, fmt.Errorf("process list from %s returned no messages", node)
	}
	infos := messages[0].GetProcesses()
	processes := make([]domain.ProcessSnapshot, len(infos))
	for index, info := range infos {
		processes[index] = domain.ProcessSnapshot{
			PID:            info.GetPid(),
			PPID:           info.GetPpid(),
			State:          info.GetState(),
			Threads:        info.GetThreads(),
			CPUTime:        info.GetCpuTime(),
			VirtualMemory:  info.GetVirtualMemory(),
			ResidentMemory: info.GetResidentMemory(),
			Command:        info.GetCommand(),
			Executable:     info.GetExecutable(),
			Args:           info.GetArgs(),
			Label:          info.GetLabel(),
		}
	}
	sort.SliceStable(processes, func(i, j int) bool {
		return processes[i].PID < processes[j].PID
	})

	return domain.ProcessSet{Processes: processes}, nil
}
