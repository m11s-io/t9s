package domain

import "time"

// ClusterHealthRequest captures the inputs for a server-side cluster health
// check. ControlPlaneNodes and WorkerNodes must be IP addresses: the Talos
// server rejects hostnames. WaitTimeout bounds how long the server waits for
// the cluster to become healthy.
type ClusterHealthRequest struct {
	ControlPlaneNodes []string
	WorkerNodes       []string
	WaitTimeout       time.Duration
}

// ClusterHealthProgress is one line of the health-check transcript. EOF is set
// when the server completed the check successfully; Err carries a
// display-safe failure reason.
type ClusterHealthProgress struct {
	Message string
	EOF     bool
	Err     string
}
