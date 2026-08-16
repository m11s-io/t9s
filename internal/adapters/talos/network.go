package talos

import (
	"context"
	"fmt"
	"sort"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

type networkClient interface {
	Links(ctx context.Context, node string) ([]*network.LinkStatus, error)
	Addresses(ctx context.Context, node string) ([]*network.AddressStatus, error)
	Routes(ctx context.Context, node string) ([]*network.RouteStatus, error)
}

type machineryNetworkClient struct{ client *talosclient.Client }

func (c machineryNetworkClient) Links(ctx context.Context, node string) ([]*network.LinkStatus, error) {
	list, err := safe.StateList[*network.LinkStatus](
		talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(network.NamespaceName, network.LinkStatusType, "", resource.VersionUndefined),
	)
	if err != nil {
		return nil, err
	}
	var links []*network.LinkStatus
	for link := range list.All() {
		links = append(links, link)
	}
	return links, nil
}

func (c machineryNetworkClient) Addresses(ctx context.Context, node string) ([]*network.AddressStatus, error) {
	list, err := safe.StateList[*network.AddressStatus](
		talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(network.NamespaceName, network.AddressStatusType, "", resource.VersionUndefined),
	)
	if err != nil {
		return nil, err
	}
	var addresses []*network.AddressStatus
	for address := range list.All() {
		addresses = append(addresses, address)
	}
	return addresses, nil
}

func (c machineryNetworkClient) Routes(ctx context.Context, node string) ([]*network.RouteStatus, error) {
	list, err := safe.StateList[*network.RouteStatus](
		talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(network.NamespaceName, network.RouteStatusType, "", resource.VersionUndefined),
	)
	if err != nil {
		return nil, err
	}
	var routes []*network.RouteStatus
	for route := range list.All() {
		routes = append(routes, route)
	}
	return routes, nil
}

type networkReader struct {
	client networkClient
}

func newNetworkReader(client networkClient) ports.NetworkReader {
	return &networkReader{client: client}
}

func isInetFamily(family nethelpers.Family) bool {
	return family == nethelpers.FamilyInet4 || family == nethelpers.FamilyInet6
}

func (r *networkReader) List(ctx context.Context, node string) (domain.NetworkSet, error) {
	links, err := r.client.Links(ctx, node)
	if err != nil {
		return domain.NetworkSet{}, fmt.Errorf("list links: %w", err)
	}

	addresses, err := r.client.Addresses(ctx, node)
	if err != nil {
		return domain.NetworkSet{}, fmt.Errorf("list addresses: %w", err)
	}
	addressesByLink := map[string][]domain.NetworkAddress{}
	for _, address := range addresses {
		spec := address.TypedSpec()
		if !isInetFamily(spec.Family) {
			continue
		}
		addressesByLink[spec.LinkName] = append(addressesByLink[spec.LinkName], domain.NetworkAddress{
			Address: spec.Address.String(),
			Scope:   spec.Scope.String(),
		})
	}

	routes, err := r.client.Routes(ctx, node)
	if err != nil {
		return domain.NetworkSet{}, fmt.Errorf("list routes: %w", err)
	}
	routesByLink := map[string][]domain.NetworkRoute{}
	for _, route := range routes {
		spec := route.TypedSpec()
		if !isInetFamily(spec.Family) {
			continue
		}
		gateway := ""
		if spec.Gateway.IsValid() {
			gateway = spec.Gateway.String()
		}
		routesByLink[spec.OutLinkName] = append(routesByLink[spec.OutLinkName], domain.NetworkRoute{
			Destination: spec.Destination.String(),
			Gateway:     gateway,
			Table:       spec.Table.String(),
		})
	}

	snapshots := make([]domain.LinkSnapshot, len(links))
	for index, link := range links {
		spec := link.TypedSpec()
		name := link.Metadata().ID()
		snapshots[index] = domain.LinkSnapshot{
			Name:             name,
			Type:             spec.Type.String(),
			OperationalState: spec.OperationalState.String(),
			HardwareAddr:     spec.HardwareAddr.String(),
			MTU:              spec.MTU,
			Driver:           spec.Driver,
			Addresses:        addressesByLink[name],
			Routes:           routesByLink[name],
		}
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].Name < snapshots[j].Name
	})

	return domain.NetworkSet{Links: snapshots}, nil
}
