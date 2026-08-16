package domain

type ResourceKindSnapshot struct {
	Type             string
	DisplayType      string
	DefaultNamespace string
	Aliases          []string
	Sensitive        bool
}

type ResourceKindSet struct {
	Kinds []ResourceKindSnapshot
}

type ResourceInstanceSnapshot struct {
	Namespace string
	Type      string
	ID        string
	Version   string
	Phase     string
	YAML      string
}

type ResourceInstanceSet struct {
	Instances []ResourceInstanceSnapshot
}
