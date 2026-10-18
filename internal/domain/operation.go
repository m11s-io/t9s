package domain

// OperationOutcome distinguishes an acknowledged request from observed
// completion. Losing an RPC response does not establish remote failure.
type OperationOutcome string

const (
	OperationAccepted           OperationOutcome = "accepted"
	OperationCompleted          OperationOutcome = "completed"
	OperationUnconfirmed        OperationOutcome = "unconfirmed"
	OperationRecoveryIncomplete OperationOutcome = "recovery-incomplete"
)
