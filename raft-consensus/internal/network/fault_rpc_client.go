package network

import (
	"sync"
	"sync/atomic"

	"raft-consensus/internal/raft"
)

var _ raft.RaftService = (*FaultInjectingRPCClient)(nil)

type FaultInjectingRPCClient struct {
	inner     raft.RaftService
	connected atomic.Bool
}

func NewFaultInjectingRPCClient(inner raft.RaftService) *FaultInjectingRPCClient {
	c := &FaultInjectingRPCClient{inner: inner}
	c.connected.Store(true)
	return c
}

func (c *FaultInjectingRPCClient) Disconnect() {
	c.connected.Store(false)
}

func (c *FaultInjectingRPCClient) Reconnect() {
	c.connected.Store(true)
}

func (c *FaultInjectingRPCClient) RequestVote(args *raft.RequestVoteArgs, reply *raft.RequestVoteReply) error {
	if !c.connected.Load() {
		return ErrNetworkPartition
	}
	return c.inner.RequestVote(args, reply)
}

func (c *FaultInjectingRPCClient) AppendEntries(args *raft.AppendEntriesArgs, reply *raft.AppendEntriesReply) error {
	if !c.connected.Load() {
		return ErrNetworkPartition
	}
	return c.inner.AppendEntries(args, reply)
}

type OutgoingNetworkSwitch struct {
	mu      sync.RWMutex
	proxies []*FaultInjectingRPCClient
}

func NewOutgoingNetworkSwitch(proxies ...*FaultInjectingRPCClient) *OutgoingNetworkSwitch {
	p := make([]*FaultInjectingRPCClient, len(proxies))
	copy(p, proxies)
	return &OutgoingNetworkSwitch{proxies: p}
}

// AddProxy registers a fault-injecting client so partition toggles apply to dynamically added peers.
func (s *OutgoingNetworkSwitch) AddProxy(p *FaultInjectingRPCClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proxies = append(s.proxies, p)
}

func (s *OutgoingNetworkSwitch) DisconnectOutgoing() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.proxies {
		p.Disconnect()
	}
}

func (s *OutgoingNetworkSwitch) ReconnectOutgoing() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.proxies {
		p.Reconnect()
	}
}
