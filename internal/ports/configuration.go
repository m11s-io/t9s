package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

// ConfigurationController previews and applies a machine configuration on
// one Talos node. Implementations must return only safe metadata and errors.
type ConfigurationController interface {
	Preview(context.Context, string, []byte, domain.ConfigurationMode) (domain.ConfigurationPreview, error)
	Apply(context.Context, string, []byte, domain.ConfigurationMode) (domain.ConfigurationApplyResult, error)
}
