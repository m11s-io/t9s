package domain

// ConfigurationMode selects the Talos machine configuration apply behavior.
type ConfigurationMode string

const (
	ConfigurationModeAuto     ConfigurationMode = "AUTO"
	ConfigurationModeNoReboot ConfigurationMode = "NO_REBOOT"
	ConfigurationModeStaged   ConfigurationMode = "STAGED"
)

// ConfigurationPreview contains only safe metadata from a Talos dry run.
// Fingerprint binds a prepared plan to the response without exposing its diff.
type ConfigurationPreview struct {
	Mode          ConfigurationMode
	WarningCount  int
	WarningsKnown bool
	Fingerprint   [32]byte
}

// ConfigurationApplyResult contains safe metadata from a configuration apply.
type ConfigurationApplyResult struct {
	Mode        ConfigurationMode
	Fingerprint [32]byte
}

// ConfigurationError is a stable, non-sensitive error class for Talos config
// operations. Talos response text can contain configuration secrets.
type ConfigurationError string

const (
	ConfigurationErrorUnavailable        ConfigurationError = "Talos configuration API unavailable"
	ConfigurationErrorPermissionDenied   ConfigurationError = "Talos rejected configuration permission"
	ConfigurationErrorInvalid            ConfigurationError = "Talos rejected configuration validation"
	ConfigurationErrorUnsupported        ConfigurationError = "Talos configuration mode unsupported"
	ConfigurationErrorFailedPrecondition ConfigurationError = "Talos configuration precondition failed"
	ConfigurationErrorCanceled           ConfigurationError = "Talos configuration request canceled"
	ConfigurationErrorDeadline           ConfigurationError = "Talos configuration request timed out"
	ConfigurationErrorResponseInvalid    ConfigurationError = "Talos returned an invalid configuration response"
	ConfigurationErrorResponseTooLarge   ConfigurationError = "Talos configuration preview response exceeded the size limit"
	ConfigurationErrorUnknown            ConfigurationError = "Talos configuration request failed"
)

func (e ConfigurationError) Error() string { return string(e) }
