package domain

type Severity uint8

const (
	SeverityUnknown Severity = iota
	SeverityHealthy
	SeverityWarning
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityHealthy:
		return "healthy"
	case SeverityWarning:
		return "warning"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

type Diagnosis struct {
	RuleID       string
	Severity     Severity
	Summary      string
	Evidence     []string
	ResourceKind string
	ResourceID   string
	ResourceName string
}
