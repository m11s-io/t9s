package talos

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const maxConfigurationResponseBytes = 1 << 20

type configurationClient interface {
	ApplyConfiguration(context.Context, *machineapi.ApplyConfigurationRequest, ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error)
}

type machineryConfigurationClient struct{ client *talosclient.Client }

func (c machineryConfigurationClient) ApplyConfiguration(ctx context.Context, request *machineapi.ApplyConfigurationRequest, options ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	return c.client.ApplyConfiguration(ctx, request, options...)
}

type configurationController struct{ client configurationClient }

var _ ports.ConfigurationController = (*configurationController)(nil)

func newConfigurationController(client configurationClient) ports.ConfigurationController {
	return &configurationController{client: client}
}

func (c *configurationController) Preview(ctx context.Context, node string, data []byte, mode domain.ConfigurationMode) (domain.ConfigurationPreview, error) {
	requestMode, err := talosConfigurationMode(mode)
	if err != nil {
		return domain.ConfigurationPreview{}, err
	}
	response, err := c.client.ApplyConfiguration(talosclient.WithNode(ctx, node), &machineapi.ApplyConfigurationRequest{
		Data: data, Mode: requestMode, DryRun: true,
	})
	if err != nil {
		return domain.ConfigurationPreview{}, safeConfigurationError(err)
	}
	if response == nil || len(response.GetMessages()) == 0 {
		return domain.ConfigurationPreview{}, domain.ConfigurationErrorResponseInvalid
	}
	fingerprint, err := configurationResponseFingerprint(response)
	if err != nil {
		return domain.ConfigurationPreview{}, err
	}
	warnings := 0
	for _, message := range response.GetMessages() {
		if message == nil || configurationModeFromTalos(message.GetMode()) != expectedResponseMode(mode) {
			return domain.ConfigurationPreview{}, domain.ConfigurationErrorResponseInvalid
		}
		warnings += len(message.GetWarnings())
	}
	return domain.ConfigurationPreview{Mode: expectedResponseMode(mode), WarningCount: warnings, Fingerprint: fingerprint}, nil
}

func (c *configurationController) Apply(ctx context.Context, node string, data []byte, mode domain.ConfigurationMode) (domain.ConfigurationApplyResult, error) {
	requestMode, err := talosConfigurationMode(mode)
	if err != nil {
		return domain.ConfigurationApplyResult{}, err
	}
	response, err := c.client.ApplyConfiguration(talosclient.WithNode(ctx, node), &machineapi.ApplyConfigurationRequest{
		Data: data, Mode: requestMode, DryRun: false,
	})
	if err != nil {
		return domain.ConfigurationApplyResult{}, safeConfigurationError(err)
	}
	if response == nil || len(response.GetMessages()) == 0 {
		return domain.ConfigurationApplyResult{}, domain.ConfigurationErrorResponseInvalid
	}
	fingerprint, err := configurationResponseFingerprint(response)
	if err != nil {
		return domain.ConfigurationApplyResult{}, err
	}
	for _, message := range response.GetMessages() {
		if message == nil || configurationModeFromTalos(message.GetMode()) != expectedResponseMode(mode) {
			return domain.ConfigurationApplyResult{}, domain.ConfigurationErrorResponseInvalid
		}
	}
	return domain.ConfigurationApplyResult{Mode: expectedResponseMode(mode), Fingerprint: fingerprint}, nil
}

func talosConfigurationMode(mode domain.ConfigurationMode) (machineapi.ApplyConfigurationRequest_Mode, error) {
	switch mode {
	case domain.ConfigurationModeAuto:
		return machineapi.ApplyConfigurationRequest_AUTO, nil
	case domain.ConfigurationModeNoReboot:
		return machineapi.ApplyConfigurationRequest_NO_REBOOT, nil
	case domain.ConfigurationModeStaged:
		return machineapi.ApplyConfigurationRequest_STAGED, nil
	default:
		return 0, domain.ConfigurationErrorUnsupported
	}
}

func expectedResponseMode(requested domain.ConfigurationMode) domain.ConfigurationMode {
	if requested == domain.ConfigurationModeAuto {
		return domain.ConfigurationModeNoReboot
	}
	return requested
}

func configurationModeFromTalos(mode machineapi.ApplyConfigurationRequest_Mode) domain.ConfigurationMode {
	switch mode {
	case machineapi.ApplyConfigurationRequest_AUTO:
		return domain.ConfigurationModeAuto
	case machineapi.ApplyConfigurationRequest_NO_REBOOT:
		return domain.ConfigurationModeNoReboot
	case machineapi.ApplyConfigurationRequest_STAGED:
		return domain.ConfigurationModeStaged
	default:
		return ""
	}
}

func configurationResponseFingerprint(response *machineapi.ApplyConfigurationResponse) ([32]byte, error) {
	if proto.Size(response) > maxConfigurationResponseBytes {
		return [32]byte{}, domain.ConfigurationErrorResponseTooLarge
	}
	encoded, err := proto.Marshal(response)
	if err != nil {
		return [32]byte{}, domain.ConfigurationErrorResponseInvalid
	}
	return sha256.Sum256(encoded), nil
}

func safeConfigurationError(err error) error {
	if errors.Is(err, context.Canceled) {
		return domain.ConfigurationErrorCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.ConfigurationErrorDeadline
	}
	switch status.Code(err) {
	case codes.Canceled:
		return domain.ConfigurationErrorCanceled
	case codes.DeadlineExceeded:
		return domain.ConfigurationErrorDeadline
	case codes.PermissionDenied, codes.Unauthenticated:
		return domain.ConfigurationErrorPermissionDenied
	case codes.InvalidArgument:
		return domain.ConfigurationErrorInvalid
	case codes.Unimplemented:
		return domain.ConfigurationErrorUnsupported
	case codes.FailedPrecondition:
		return domain.ConfigurationErrorFailedPrecondition
	case codes.Unavailable:
		return domain.ConfigurationErrorUnavailable
	default:
		return domain.ConfigurationErrorUnknown
	}
}
