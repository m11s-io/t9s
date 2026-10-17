package application

import (
	"context"
	"errors"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type CPUState struct {
	Status LoadStatus
	Node   string
	Value  domain.CPUSnapshot
	Err    string
}
type OpenCPU struct{ Node string }

func (OpenCPU) applicationMessage() {}

type RefreshCPU struct{}

func (RefreshCPU) applicationMessage() {}

type CloseCPU struct{}

func (CloseCPU) applicationMessage() {}

type CPULoaded struct {
	Generation, RequestID uint64
	Node                  string
	Value                 domain.CPUSnapshot
	Err                   error
}

func (CPULoaded) applicationMessage() {}

func updateCPU(model Model, message Message) (Model, Effect, bool) {
	switch msg := message.(type) {
	case OpenCPU:
		closeCPURequest(&model)
		model.CPU = CPUState{Node: msg.Node}
		next, effect := startCPUSample(model)
		return next, effect, true
	case RefreshCPU:
		if model.CPU.Node == "" {
			return model, nil, true
		}
		next, effect := startCPUSample(model)
		return next, effect, true
	case CloseCPU:
		closeCPURequest(&model)
		model.CPU = CPUState{}
		return model, nil, true
	case CPULoaded:
		if msg.Generation != model.Generation || msg.RequestID != model.cpuRequestID || msg.Node != model.CPU.Node || model.CPU.Status != Loading {
			return model, nil, true
		}
		if model.cpuCancel != nil {
			model.cpuCancel()
			model.cpuCancel = nil
		}
		model.CPU.Value = msg.Value
		model.CPU.Status = Ready
		if len(msg.Value.Problems) > 0 || !msg.Value.Aggregate.Known {
			model.CPU.Status = Partial
		}
		if msg.Err != nil {
			model.CPU.Value.Aggregate = domain.CPUUtilization{}
			model.CPU.Value.Cores = append([]domain.CPUCoreSnapshot(nil), msg.Value.Cores...)
			for i := range model.CPU.Value.Cores {
				model.CPU.Value.Cores[i].CPUUtilization = domain.CPUUtilization{}
			}
			model.CPU.Status = Failed
			model.CPU.Err = "CPU sample unavailable"
			if errors.Is(msg.Err, context.Canceled) {
				model.CPU.Err = "CPU sample canceled"
			}
			if msg.Value.ProcessCountsKnown {
				model.CPU.Status = Partial
			}
		}
		return model, nil, true
	}
	return model, nil, false
}
func startCPUSample(model Model) (Model, Effect) {
	if model.cpuCancel != nil {
		model.cpuCancel()
	}
	model.cpuRequestID++
	lifetime, cancel := context.WithCancel(context.Background())
	model.cpuCancel = cancel
	model.CPU.Status = Loading
	model.CPU.Err = ""
	generation, id, node, reader := model.Generation, model.cpuRequestID, model.CPU.Node, model.cpuReader
	return model, func(effectCtx context.Context, _ Dependencies) Message {
		stop := context.AfterFunc(effectCtx, cancel)
		if effectCtx.Err() != nil {
			cancel()
		}
		ctx, timeoutCancel := context.WithTimeout(lifetime, 10*time.Second)
		defer func() { stop(); timeoutCancel(); cancel() }()
		var value domain.CPUSnapshot
		var err error
		if reader == nil {
			err = errors.New("CPU reader unavailable")
		} else if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			value, err = reader.Sample(ctx, node)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return CPULoaded{Generation: generation, RequestID: id, Node: node, Value: value, Err: err}
	}
}
func closeCPURequest(model *Model) {
	if model.cpuCancel != nil {
		model.cpuCancel()
		model.cpuCancel = nil
	}
	model.cpuRequestID++
}

func (s managedSession) CPU() ports.CPUReader {
	if s.cpu == nil {
		return nil
	}
	return boundCPUReader{CPUReader: s.cpu, ctx: s.ctx}
}

type boundCPUReader struct {
	ports.CPUReader
	ctx context.Context
}

func (r boundCPUReader) Sample(callCtx context.Context, node string) (domain.CPUSnapshot, error) {
	ctx, cancel := context.WithCancel(r.ctx)
	stop := context.AfterFunc(callCtx, cancel)
	if callCtx.Err() != nil {
		cancel()
	}
	defer func() { stop(); cancel() }()
	return r.CPUReader.Sample(ctx, node)
}
