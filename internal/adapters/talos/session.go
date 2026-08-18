package talos

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"k8s.io/klog/v2"
)

type sessionFactory struct {
	configPaths []string
}

var _ ports.SessionFactory = (*sessionFactory)(nil)

// NewSessionFactory creates Talos sessions using one or more talosconfig files.
func NewSessionFactory(configPaths ...string) ports.SessionFactory {
	klog.SetOutputBySeverity("INFO", io.Discard)
	return &sessionFactory{configPaths: append([]string(nil), configPaths...)}
}

func (f *sessionFactory) Open(ctx context.Context, contextName string) (ports.Session, error) {
	config, err := openTalosconfig(f.configPaths...)
	if err != nil {
		return nil, fmt.Errorf("open Talos session: %w", err)
	}

	client, err := talosclient.New(ctx,
		talosclient.WithConfig(config),
		talosclient.WithContextName(contextName),
	)
	if err != nil {
		return nil, fmt.Errorf("open Talos session: %w", err)
	}

	return &session{
		client:            client,
		nodes:             newNodeReader(&machineryAPI{client: client}, time.Now),
		nodeController:    newNodeController(machineryNodeControlClient{client: client}),
		serviceController: newServiceController(machineryServiceControlClient{client: client}),
		services:          newServiceReader(client, time.Now),
		logs:              newServiceLogReader(machineryLogClient{client: client}),
		events:            newEventReader(&machineryAPI{client: client}, machineryEventsClient{client: client}, time.Now, eventFetchTimeout),
		etcd:              newEtcdReader(machineryEtcdClient{client: client}),
		processes:         newProcessReader(machineryProcessClient{client: client}),
		disks:             newDiskReader(machineryDiskClient{client: client}),
		network:           newNetworkReader(machineryNetworkClient{client: client}),
		resourceKinds:     newResourceKindReader(machineryResourceKindClient{client: client}),
		resourceInstances: newResourceInstanceReader(machineryResourceInstanceClient{client: client}),
	}, nil
}

type session struct {
	client            *talosclient.Client
	nodes             ports.NodeReader
	nodeController    ports.NodeController
	serviceController ports.ServiceController
	services          ports.ServiceReader
	logs              ports.ServiceLogReader
	events            ports.EventReader
	etcd              ports.EtcdReader
	processes         ports.ProcessReader
	disks             ports.DiskReader
	network           ports.NetworkReader
	resourceKinds     ports.ResourceKindReader
	resourceInstances ports.ResourceInstanceReader
}

var _ ports.Session = (*session)(nil)

func (s *session) Nodes() ports.NodeReader { return s.nodes }

func (s *session) NodeActions() ports.NodeController { return s.nodeController }

func (s *session) ServiceActions() ports.ServiceController { return s.serviceController }

func (s *session) Services() ports.ServiceReader { return s.services }

func (s *session) ServiceLogs() ports.ServiceLogReader { return s.logs }

func (s *session) Events() ports.EventReader { return s.events }

func (s *session) Etcd() ports.EtcdReader { return s.etcd }

func (s *session) Processes() ports.ProcessReader { return s.processes }

func (s *session) Disks() ports.DiskReader { return s.disks }

func (s *session) Network() ports.NetworkReader { return s.network }

func (s *session) ResourceKinds() ports.ResourceKindReader { return s.resourceKinds }

func (s *session) Resources() ports.ResourceInstanceReader { return s.resourceInstances }

func (s *session) Close() error {
	return s.client.Close()
}
