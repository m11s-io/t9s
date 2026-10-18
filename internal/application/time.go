package application

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"unicode"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type TimeState struct {
	Status      LoadStatus
	Node        string
	Value       domain.TimeSnapshot
	Err         string
	CheckStatus LoadStatus
	Check       domain.TimeCheckResult
	CheckErr    string
}
type OpenTime struct{ Node string }

func (OpenTime) applicationMessage() {}

type RefreshTime struct{}

func (RefreshTime) applicationMessage() {}

type CloseTime struct{}

func (CloseTime) applicationMessage() {}

type CheckTime struct{ Server string }

func (CheckTime) applicationMessage() {}

type TimeLoaded struct {
	Generation, RequestID uint64
	Node                  string
	Value                 domain.TimeSnapshot
	Err                   error
}

func (TimeLoaded) applicationMessage() {}

type TimeChecked struct {
	Generation, RequestID uint64
	Node, Server          string
	Value                 domain.TimeCheckResult
	Err                   error
}

func (TimeChecked) applicationMessage() {}

func updateTime(model Model, message Message) (Model, Effect, bool) {
	switch msg := message.(type) {
	case OpenTime:
		closeTimeRequests(&model)
		model.Time = TimeState{Node: msg.Node}
		next, effect := startTimeRead(model)
		return next, effect, true
	case RefreshTime:
		if model.Time.Node == "" {
			return model, nil, true
		}
		next, effect := startTimeRead(model)
		return next, effect, true
	case CloseTime:
		closeTimeRequests(&model)
		model.Time = TimeState{}
		return model, nil, true
	case CheckTime:
		if model.Time.Node == "" {
			return model, nil, true
		}
		server, err := ValidateTimeServer(msg.Server)
		if err != nil {
			model.Time.CheckErr = err.Error()
			return model, nil, true
		}
		model.Time.CheckStatus = Loading
		model.Time.Check = domain.TimeCheckResult{TimeSample: domain.TimeSample{Server: server}}
		model.Time.CheckErr = ""
		generation, node, reader := model.Generation, model.Time.Node, model.timeReader
		return model, func(ctx context.Context, _ Dependencies) Message {
			var value domain.TimeCheckResult
			var err error
			if reader == nil {
				err = errors.New("time reader unavailable")
			} else {
				value, err = reader.Check(ctx, node, server)
			}
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			return TimeChecked{Generation: generation, Node: node, Server: server, Value: value, Err: err}
		}, true
	case TimeLoaded:
		if msg.Generation != model.Generation || msg.Node != model.Time.Node || model.Time.Status != Loading {
			return model, nil, true
		}

		if msg.Err != nil {
			model.Time.Status = Failed
			model.Time.Err = "time status unavailable"
			return model, nil, true
		}
		model.Time.Value = msg.Value
		model.Time.Status = Ready
		if len(msg.Value.Problems) > 0 {
			model.Time.Status = Partial
		}
		return model, nil, true
	case TimeChecked:
		if msg.Generation != model.Generation || msg.Node != model.Time.Node || msg.Server != model.Time.Check.Server || model.Time.CheckStatus != Loading {
			return model, nil, true
		}

		if msg.Err != nil {
			model.Time.CheckStatus = Failed
			model.Time.CheckErr = "time check unavailable"
			if errors.Is(msg.Err, ports.ErrTimePermissionDenied) {
				model.Time.CheckErr = "time check permission denied"
			}
			return model, nil, true
		}
		model.Time.Check = msg.Value
		model.Time.Check.Server = msg.Server
		model.Time.CheckStatus = Ready
		if len(msg.Value.Problems) > 0 {
			model.Time.CheckStatus = Partial
		}
		return model, nil, true
	}
	return model, nil, false
}
func startTimeRead(model Model) (Model, Effect) {
	model.Time.Status = Loading
	model.Time.Err = ""
	generation, node, reader := model.Generation, model.Time.Node, model.timeReader
	return model, func(ctx context.Context, _ Dependencies) Message {
		var value domain.TimeSnapshot
		var err error
		if reader == nil {
			err = errors.New("time reader unavailable")
		} else {
			value, err = reader.Read(ctx, node)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return TimeLoaded{Generation: generation, Node: node, Value: value, Err: err}
	}
}

func closeTimeRequests(model *Model) {
	*model = closeRead(*model, "time")
	*model = closeRead(*model, "time-check")
}

// ValidateTimeServer accepts a hostname or a bare IP address, never a URL,
// port, path, or whitespace/control-bearing value.
func ValidateTimeServer(input string) (string, error) {
	invalid := fmt.Errorf("enter a hostname or bare IP address")
	for _, r := range input {
		if unicode.IsControl(r) {
			return "", invalid
		}
	}
	server := strings.TrimSpace(input)
	if server == "" || strings.ContainsFunc(server, unicode.IsSpace) {
		return "", invalid
	}
	if addr, err := netip.ParseAddr(server); err == nil && addr.Zone() == "" {
		return server, nil
	}
	host := strings.TrimSuffix(server, ".")
	if len(host) == 0 || len(host) > 253 {
		return "", invalid
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", invalid
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return "", invalid
			}
		}
	}
	return server, nil
}
