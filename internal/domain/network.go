package domain

type NetworkAddress struct {
	Address string
	Scope   string
}

type NetworkRoute struct {
	Destination string
	Gateway     string
	Table       string
}

type LinkSnapshot struct {
	Name             string
	Type             string
	OperationalState string
	HardwareAddr     string
	MTU              uint32
	Driver           string
	Addresses        []NetworkAddress
	Routes           []NetworkRoute
}

type NetworkSet struct {
	Links []LinkSnapshot
}
