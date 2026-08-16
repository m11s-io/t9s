package talos

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNetworkClient struct {
	links     []*network.LinkStatus
	addresses []*network.AddressStatus
	routes    []*network.RouteStatus
	err       error
}

func (c *fakeNetworkClient) Links(context.Context, string) ([]*network.LinkStatus, error) {
	return c.links, c.err
}

func (c *fakeNetworkClient) Addresses(context.Context, string) ([]*network.AddressStatus, error) {
	return c.addresses, nil
}

func (c *fakeNetworkClient) Routes(context.Context, string) ([]*network.RouteStatus, error) {
	return c.routes, nil
}

func newTestLink(name string, mtu uint32) *network.LinkStatus {
	link := network.NewLinkStatus(network.NamespaceName, name)
	*link.TypedSpec() = network.LinkStatusSpec{
		Type:             nethelpers.LinkEther,
		OperationalState: nethelpers.OperStateUp,
		MTU:              mtu,
	}
	return link
}

func newTestAddress(linkName, cidr string) *network.AddressStatus {
	address := network.NewAddressStatus(network.NamespaceName, linkName+"/"+cidr)
	prefix := netip.MustParsePrefix(cidr)
	family := nethelpers.FamilyInet4
	if prefix.Addr().Is6() {
		family = nethelpers.FamilyInet6
	}
	*address.TypedSpec() = network.AddressStatusSpec{
		Address:  prefix,
		LinkName: linkName,
		Family:   family,
		Scope:    nethelpers.ScopeGlobal,
	}
	return address
}

func TestNetworkReaderListJoinsAddressesOntoLinksAndSortsByName(t *testing.T) {
	client := &fakeNetworkClient{
		links: []*network.LinkStatus{
			newTestLink("eth1", 1500),
			newTestLink("eth0", 1500),
		},
		addresses: []*network.AddressStatus{
			newTestAddress("eth0", "10.0.0.5/24"),
		},
	}
	reader := newNetworkReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	require.Len(t, set.Links, 2)
	assert.Equal(t, "eth0", set.Links[0].Name)
	require.Len(t, set.Links[0].Addresses, 1)
	assert.Equal(t, "10.0.0.5/24", set.Links[0].Addresses[0].Address)
	assert.Equal(t, "eth1", set.Links[1].Name)
	assert.Empty(t, set.Links[1].Addresses)
}

func TestNetworkReaderListFiltersNonInetAddressFamilies(t *testing.T) {
	linkLocal := newTestAddress("eth0", "fe80::1/64")
	*linkLocal.TypedSpec() = network.AddressStatusSpec{
		Address:  netip.MustParsePrefix("fe80::1/64"),
		LinkName: "eth0",
		Family:   nethelpers.Family(0), // neither FamilyInet4 nor FamilyInet6
		Scope:    nethelpers.ScopeLink,
	}
	client := &fakeNetworkClient{
		links:     []*network.LinkStatus{newTestLink("eth0", 1500)},
		addresses: []*network.AddressStatus{linkLocal},
	}
	reader := newNetworkReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	require.Len(t, set.Links, 1)
	assert.Empty(t, set.Links[0].Addresses)
}

func TestNetworkReaderListReturnsErrorWhenLinksCallFails(t *testing.T) {
	client := &fakeNetworkClient{err: errors.New("unreachable")}
	reader := newNetworkReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}
