package talos

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

// routeGateway resolves a route's gateway. Native multipath (ECMP) routes
// leave the top-level Gateway unset and carry their next-hops instead, so the
// next-hop gateways are joined for display rather than rendered blank.
func routeGateway(spec *network.RouteStatusSpec) string {
	if spec.Gateway.IsValid() {
		return spec.Gateway.String()
	}
	gateways := make([]string, 0, len(spec.NextHops))
	for _, hop := range spec.NextHops {
		if hop.Gateway.IsValid() {
			gateways = append(gateways, hop.Gateway.String())
		}
	}

	return strings.Join(gateways, ", ")
}

// routeLinkNames resolves the link(s) a route belongs to. Multipath routes
// leave OutLinkName unset and name a link per next-hop, so each distinct
// next-hop link must receive the route or it disappears from every link view.
func routeLinkNames(spec *network.RouteStatusSpec) []string {
	if spec.OutLinkName != "" {
		return []string{spec.OutLinkName}
	}
	seen := make(map[string]struct{}, len(spec.NextHops))
	names := make([]string, 0, len(spec.NextHops))
	for _, hop := range spec.NextHops {
		if hop.OutLinkName == "" {
			continue
		}
		if _, ok := seen[hop.OutLinkName]; ok {
			continue
		}
		seen[hop.OutLinkName] = struct{}{}
		names = append(names, hop.OutLinkName)
	}
	if len(names) == 0 {
		return []string{""}
	}

	return names
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
		gateway := routeGateway(spec)
		table := spec.Table.String()
		for _, linkName := range routeLinkNames(spec) {
			routesByLink[linkName] = append(routesByLink[linkName], domain.NetworkRoute{
				Destination: spec.Destination.String(),
				Gateway:     gateway,
				Table:       table,
			})
		}
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
