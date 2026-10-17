package talos

import (
	"context"
	"fmt"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	timeapi "github.com/siderolabs/talos/pkg/machinery/api/time"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	timeresource "github.com/siderolabs/talos/pkg/machinery/resources/time"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type timeClient interface {
	Time(context.Context, string) (*timeapi.TimeResponse, error)
	TimeCheck(context.Context, string, string) (*timeapi.TimeResponse, error)
	TimeStatus(context.Context, string) (*timeresource.Status, error)
}
type machineryTimeClient struct{ client *talosclient.Client }

func (c machineryTimeClient) Time(ctx context.Context, node string) (*timeapi.TimeResponse, error) {
	return c.client.Time(talosclient.WithNode(ctx, node))
}
func (c machineryTimeClient) TimeCheck(ctx context.Context, node, server string) (*timeapi.TimeResponse, error) {
	return c.client.TimeCheck(talosclient.WithNode(ctx, node), server)
}
func (c machineryTimeClient) TimeStatus(ctx context.Context, node string) (*timeresource.Status, error) {
	return safe.StateGet[*timeresource.Status](talosclient.WithNode(ctx, node), c.client.COSI, timeresource.NewStatus().Metadata())
}

type timeReader struct{ client timeClient }

func newTimeReader(c timeClient) ports.TimeReader { return &timeReader{client: c} }
func (r *timeReader) Read(ctx context.Context, node string) (domain.TimeSnapshot, error) {
	var snapshot domain.TimeSnapshot
	// Status is an independent observation: a configured-server RPC failure must
	// never erase a synchronization state which the node already reported.
	state, err := r.client.TimeStatus(ctx, node)
	if err != nil || state == nil {
		snapshot.Problems = append(snapshot.Problems, "time synchronization status unavailable")
	} else {
		snapshot.SyncKnown = true
		snapshot.Synced = state.TypedSpec().Synced
		snapshot.SyncDisabled = state.TypedSpec().SyncDisabled
	}
	response, err := r.client.Time(ctx, node)
	if ctx.Err() != nil {
		return domain.TimeSnapshot{}, ctx.Err()
	}
	if err != nil {
		snapshot.Problems = append(snapshot.Problems, "configured server measurement unavailable")
		return snapshot, nil
	}
	sample := readTimeSample(response)
	sample.Problems = append(snapshot.Problems, sample.Problems...)
	snapshot.TimeSample = sample
	return snapshot, nil
}
func (r *timeReader) Check(ctx context.Context, node, server string) (domain.TimeCheckResult, error) {
	response, err := r.client.TimeCheck(ctx, node, server)
	if ctx.Err() != nil {
		return domain.TimeCheckResult{}, ctx.Err()
	}
	if err != nil {
		if status.Code(err) == codes.PermissionDenied {
			return domain.TimeCheckResult{}, fmt.Errorf("time check: %w", ports.ErrTimePermissionDenied)
		}
		return domain.TimeCheckResult{}, fmt.Errorf("time check unavailable")
	}
	sample := readTimeSample(response)
	// Keep the explicit target visible even if the server returned no sample.
	sample.Server = server
	return domain.TimeCheckResult{TimeSample: sample}, nil
}
func readTimeSample(response *timeapi.TimeResponse) domain.TimeSample {
	var sample domain.TimeSample
	messages := response.GetMessages()
	if len(messages) == 0 || messages[0] == nil {
		sample.Problems = []string{"time measurement unavailable"}
		return sample
	}
	message := messages[0]
	sample.Server = message.GetServer()
	if message.GetMetadata().GetError() != "" {
		sample.Problems = []string{"node time measurement failed"}
		return sample
	}
	sample.LocalTime, sample.LocalTimeValid = validTime(message.GetLocaltime())
	sample.RemoteTime, sample.RemoteTimeValid = validTime(message.GetRemotetime())
	if !sample.LocalTimeValid {
		sample.Problems = append(sample.Problems, "node UTC time unknown or invalid")
	}
	if !sample.RemoteTimeValid {
		sample.Problems = append(sample.Problems, "server UTC time unknown or invalid")
	}
	if sample.LocalTimeValid && sample.RemoteTimeValid {
		sample.Difference = sample.RemoteTime.Sub(sample.LocalTime)
		// time.Duration is narrower than the protobuf timestamp range. Do not
		// display a saturated duration as a valid measurement.
		if sample.LocalTime.Add(sample.Difference).Equal(sample.RemoteTime) {
			sample.DifferenceValid = true
		} else {
			sample.Problems = append(sample.Problems, "sampled difference outside supported range")
		}
	}
	return sample
}
func validTime(ts *timestamppb.Timestamp) (time.Time, bool) {
	if ts == nil || ts.CheckValid() != nil {
		return time.Time{}, false
	}
	return ts.AsTime().UTC(), true
}
