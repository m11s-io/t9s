package talos

import (
	"context"
	"fmt"
	"sort"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"golang.org/x/sync/errgroup"
)

type etcdClient interface {
	EtcdMemberList(ctx context.Context, node string, req *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error)
	EtcdStatus(ctx context.Context, node string) (*machineapi.EtcdStatusResponse, error)
}

type machineryEtcdClient struct{ client *talosclient.Client }

func (c machineryEtcdClient) EtcdMemberList(ctx context.Context, node string, req *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error) {
	return c.client.EtcdMemberList(talosclient.WithNode(ctx, node), req)
}

func (c machineryEtcdClient) EtcdStatus(ctx context.Context, node string) (*machineapi.EtcdStatusResponse, error) {
	return c.client.EtcdStatus(talosclient.WithNode(ctx, node))
}

type etcdReader struct {
	client etcdClient
}

func newEtcdReader(client etcdClient) ports.EtcdReader {
	return &etcdReader{client: client}
}

func (r *etcdReader) List(ctx context.Context, controlPlaneNodes []string) (domain.EtcdSet, error) {
	members, err := r.fetchMembership(ctx, controlPlaneNodes)
	if err != nil {
		return domain.EtcdSet{}, err
	}

	byID := make(map[uint64]int, len(members))
	for index, member := range members {
		byID[member.MemberID] = index
	}

	semaphore := make(chan struct{}, maxConcurrentNodeInspections)
	group, groupCtx := errgroup.WithContext(ctx)

	for _, node := range controlPlaneNodes {
		if err := ctx.Err(); err != nil {
			_ = group.Wait()
			return domain.EtcdSet{}, err
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			_ = group.Wait()
			return domain.EtcdSet{}, ctx.Err()
		}

		node := node
		group.Go(func() error {
			defer func() { <-semaphore }()
			r.applyStatus(groupCtx, node, members, byID)
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return domain.EtcdSet{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.EtcdSet{}, err
	}

	sort.SliceStable(members, func(i, j int) bool {
		return members[i].Hostname < members[j].Hostname
	})

	return domain.EtcdSet{Members: members}, nil
}

func (r *etcdReader) fetchMembership(ctx context.Context, controlPlaneNodes []string) ([]domain.EtcdMemberSnapshot, error) {
	var lastErr error
	for _, node := range controlPlaneNodes {
		response, err := r.client.EtcdMemberList(ctx, node, &machineapi.EtcdMemberListRequest{})
		if err != nil {
			lastErr = err
			continue
		}
		messages := response.GetMessages()
		if len(messages) == 0 {
			lastErr = fmt.Errorf("etcd member list from %s returned no messages", node)
			continue
		}
		members := messages[0].GetMembers()
		snapshots := make([]domain.EtcdMemberSnapshot, len(members))
		for index, member := range members {
			snapshots[index] = domain.EtcdMemberSnapshot{
				Hostname:   member.GetHostname(),
				MemberID:   member.GetId(),
				IsLearner:  member.GetIsLearner(),
				ClientURLs: append([]string(nil), member.GetClientUrls()...),
				PeerURLs:   append([]string(nil), member.GetPeerUrls()...),
			}
		}
		return snapshots, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no control-plane nodes available for etcd member list")
	}
	return nil, fmt.Errorf("list etcd members: %w", lastErr)
}

func (r *etcdReader) applyStatus(ctx context.Context, node string, members []domain.EtcdMemberSnapshot, byID map[uint64]int) {
	response, err := r.client.EtcdStatus(ctx, node)
	if err != nil {
		return
	}
	for _, status := range response.GetMessages() {
		memberStatus := status.GetMemberStatus()
		if memberStatus == nil {
			continue
		}
		index, ok := byID[memberStatus.GetMemberId()]
		if !ok {
			continue
		}
		members[index].StatusKnown = true
		members[index].IsLeader = memberStatus.GetLeader() == memberStatus.GetMemberId()
		members[index].DBSize = memberStatus.GetDbSize()
		members[index].DBSizeInUse = memberStatus.GetDbSizeInUse()
		members[index].RaftIndex = memberStatus.GetRaftIndex()
		members[index].RaftTerm = memberStatus.GetRaftTerm()
		members[index].RaftAppliedIndex = memberStatus.GetRaftAppliedIndex()
		members[index].StorageVersion = memberStatus.GetStorageVersion()
		members[index].Errors = append([]string(nil), memberStatus.GetErrors()...)
	}
}
