package domain

type ClusterContext struct {
	Name      string
	Cluster   string
	Endpoints []string
	Nodes     []string
	Current   bool
}
