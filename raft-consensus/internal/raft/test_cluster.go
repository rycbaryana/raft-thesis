package raft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var errTestNetworkPartition = errors.New("test cluster: network partition")

type testKVCommand struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

type testKVFSM struct {
	mu   sync.RWMutex
	data map[string]string
}

func newTestKVFSM() *testKVFSM {
	return &testKVFSM{data: make(map[string]string)}
}

func (m *testKVFSM) Apply(command []byte) (any, error) {
	var cmd testKVCommand
	if err := json.Unmarshal(command, &cmd); err != nil {
		return nil, fmt.Errorf("decode command: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch cmd.Op {
	case "put":
		m.data[cmd.Key] = cmd.Value
		return cmd.Value, nil
	case "incr":
		current := 0
		if raw, ok := m.data[cmd.Key]; ok && raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil {
				return nil, fmt.Errorf("decode integer for key %q: %w", cmd.Key, err)
			}
			current = v
		}
		current++
		m.data[cmd.Key] = strconv.Itoa(current)
		return current, nil
	default:
		return nil, fmt.Errorf("unknown operation %q", cmd.Op)
	}
}

func (m *testKVFSM) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[key]
	return val, ok
}

func (m *testKVFSM) Snapshot() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out
}

type testRPCClient struct {
	network *testNetwork
	from    NodeID
	to      NodeID
}

func (c *testRPCClient) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
	target, err := c.network.target(c.from, c.to)
	if err != nil {
		return err
	}
	return target.RequestVote(args, reply)
}

func (c *testRPCClient) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	target, err := c.network.target(c.from, c.to)
	if err != nil {
		return err
	}
	return target.AppendEntries(args, reply)
}

type testNetwork struct {
	mu    sync.RWMutex
	links map[NodeID]map[NodeID]bool
	nodes map[NodeID]*Raft
}

func newTestNetwork(ids []NodeID) *testNetwork {
	links := make(map[NodeID]map[NodeID]bool, len(ids))
	for _, from := range ids {
		links[from] = make(map[NodeID]bool, len(ids))
		for _, to := range ids {
			links[from][to] = from != to
		}
	}
	return &testNetwork{
		links: links,
		nodes: make(map[NodeID]*Raft, len(ids)),
	}
}

func (n *testNetwork) register(id NodeID, node *Raft) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nodes[id] = node
}

func (n *testNetwork) client(from, to NodeID) RaftService {
	return &testRPCClient{
		network: n,
		from:    from,
		to:      to,
	}
}

func (n *testNetwork) target(from, to NodeID) (*Raft, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if !n.links[from][to] {
		return nil, errTestNetworkPartition
	}
	node, ok := n.nodes[to]
	if !ok || node == nil {
		return nil, fmt.Errorf("node %d is not registered", to)
	}
	return node, nil
}

func (n *testNetwork) isolate(id NodeID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for peerID := range n.links {
		n.links[id][peerID] = false
		n.links[peerID][id] = false
	}
}

func (n *testNetwork) reconnect(id NodeID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for peerID := range n.links {
		if peerID == id {
			continue
		}
		n.links[id][peerID] = true
		n.links[peerID][id] = true
	}
}

type RaftNode struct {
	ID     string
	nodeID NodeID
	raft   *Raft
	fsm    *testKVFSM
}

func (n *RaftNode) State() State {
	n.raft.mu.Lock()
	defer n.raft.mu.Unlock()
	return n.raft.state
}

func (n *RaftNode) CommitIndex() LogIndex {
	n.raft.mu.Lock()
	defer n.raft.mu.Unlock()
	return n.raft.commitIndex
}

func (n *RaftNode) Put(ctx context.Context, key, value string) error {
	command, err := encodeTestCommand(testKVCommand{Op: "put", Key: key, Value: value})
	if err != nil {
		return err
	}
	_, _, _, err = n.raft.SubmitAndWait(ctx, command)
	return err
}

func (n *RaftNode) Incr(ctx context.Context, key string) (int, error) {
	command, err := encodeTestCommand(testKVCommand{Op: "incr", Key: key})
	if err != nil {
		return 0, err
	}
	_, _, _, err = n.raft.SubmitAndWait(ctx, command)
	if err != nil {
		return 0, err
	}
	raw, _ := n.fsm.Get(key)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("decode counter %q: %w", raw, err)
	}
	return value, nil
}

func (n *RaftNode) Get(ctx context.Context, key string) (string, error) {
	if err := n.raft.ReadBarrier(ctx); err != nil {
		return "", err
	}
	value, _ := n.fsm.Get(key)
	return value, nil
}

func (n *RaftNode) Snapshot() map[string]string {
	return n.fsm.Snapshot()
}

type TestCluster struct {
	t       *testing.T
	nodes   map[string]*RaftNode
	order   []string
	network *testNetwork
	once    sync.Once
}

func NewTestCluster(t *testing.T, size int) *TestCluster {
	t.Helper()
	require.GreaterOrEqual(t, size, 3, "cluster size must be at least 3")

	nodeIDs := make([]NodeID, 0, size)
	clusterAddrs := make(map[NodeID]string, size)
	for i := 1; i <= size; i++ {
		id := NodeID(i)
		nodeIDs = append(nodeIDs, id)
		clusterAddrs[id] = nodeName(id)
	}

	network := newTestNetwork(nodeIDs)
	tc := &TestCluster{
		t:       t,
		nodes:   make(map[string]*RaftNode, size),
		order:   make([]string, 0, size),
		network: network,
	}

	cfg := DefaultProductionConfig()
	cfg.HeartbeatInterval = 30 * time.Millisecond
	cfg.MinElectionTimeout = 120 * time.Millisecond
	cfg.MaxElectionTimeout = 260 * time.Millisecond
	cfg.RPCTimeout = 80 * time.Millisecond
	cfg.LeaderQuorumLivenessTimeout = 240 * time.Millisecond

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, id := range nodeIDs {
		id := id
		fsm := newTestKVFSM()
		factory := func(peerID NodeID, _ string) RaftService {
			return network.client(id, peerID)
		}
		rf := NewRaft(id, fsm, factory, clusterAddrs, WithConfig(cfg), WithLogger(logger))
		network.register(id, rf)

		node := &RaftNode{
			ID:     nodeName(id),
			nodeID: id,
			raft:   rf,
			fsm:    fsm,
		}
		tc.nodes[node.ID] = node
		tc.order = append(tc.order, node.ID)
	}

	t.Cleanup(tc.Shutdown)
	return tc
}

func (tc *TestCluster) Shutdown() {
	tc.once.Do(func() {
		for _, id := range tc.order {
			tc.nodes[id].raft.Stop()
		}
		for _, id := range tc.order {
			tc.nodes[id].raft.WaitStopped()
		}
	})
}

func (tc *TestCluster) Isolate(nodeID string) {
	id := parseNodeID(tc.t, nodeID)
	tc.network.isolate(id)
}

func (tc *TestCluster) Reconnect(nodeID string) {
	id := parseNodeID(tc.t, nodeID)
	tc.network.reconnect(id)
}

func (tc *TestCluster) WaitForLeader() *RaftNode {
	tc.t.Helper()
	var leader *RaftNode
	require.Eventually(tc.t, func() bool {
		leaders := tc.currentLeaders()
		if len(leaders) != 1 {
			return false
		}
		leader = leaders[0]
		return true
	}, 5*time.Second, 25*time.Millisecond, "leader not elected")
	return leader
}

func (tc *TestCluster) CurrentLeader() *RaftNode {
	leaders := tc.currentLeaders()
	if len(leaders) != 1 {
		return nil
	}
	return leaders[0]
}

func (tc *TestCluster) WaitForNewLeader(oldLeaderID string) *RaftNode {
	tc.t.Helper()
	var leader *RaftNode
	require.Eventually(tc.t, func() bool {
		leaders := tc.currentLeaders()
		if len(leaders) != 1 {
			return false
		}
		if leaders[0].ID == oldLeaderID {
			return false
		}
		leader = leaders[0]
		return true
	}, 5*time.Second, 25*time.Millisecond, "new leader not elected")
	return leader
}

func (tc *TestCluster) Node(nodeID string) *RaftNode {
	tc.t.Helper()
	node, ok := tc.nodes[nodeID]
	require.True(tc.t, ok, "node %q not found", nodeID)
	return node
}

func (tc *TestCluster) AnyFollower(excludingLeaderID string) *RaftNode {
	tc.t.Helper()
	for _, id := range tc.order {
		if id == excludingLeaderID {
			continue
		}
		return tc.nodes[id]
	}
	require.FailNow(tc.t, "follower not found")
	return nil
}

func (tc *TestCluster) currentLeaders() []*RaftNode {
	leaders := make([]*RaftNode, 0, len(tc.nodes))
	for _, id := range tc.order {
		node := tc.nodes[id]
		if node.State() == Leader {
			leaders = append(leaders, node)
		}
	}
	return leaders
}

func nodeName(id NodeID) string {
	return fmt.Sprintf("node%d", id)
}

func parseNodeID(t *testing.T, raw string) NodeID {
	t.Helper()
	normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "node")
	value, err := strconv.Atoi(normalized)
	require.NoErrorf(t, err, "invalid node id %q", raw)
	return NodeID(value)
}

func encodeTestCommand(cmd testKVCommand) ([]byte, error) {
	raw, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("encode command: %w", err)
	}
	return raw, nil
}
