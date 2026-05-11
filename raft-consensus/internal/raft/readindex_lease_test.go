package raft

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadIndexUsesLeaderLeaseFastPath(t *testing.T) {
	rf := newTestRaft()
	rf.id = 1
	rf.state = Leader
	rf.currentTerm = 2
	rf.log = []LogEntry{{Term: 0}, {Term: 2, Command: nil}}
	rf.commitIndex = 1
	rf.nextIndex[2] = 2
	rf.nextIndex[3] = 2

	var appendCalls atomic.Int32
	mockFn := func(_ *AppendEntriesArgs, reply *AppendEntriesReply) error {
		appendCalls.Add(1)
		reply.Success = true
		reply.Term = 2
		return nil
	}
	rf.peers = map[NodeID]*Peer{
		2: &Peer{Client: &raftServiceMock{appendFn: mockFn}, Role: Voter},
		3: &Peer{Client: &raftServiceMock{appendFn: mockFn}, Role: Voter},
	}

	now := time.Now()
	rf.lastHeartbeatAck[2] = now
	rf.lastHeartbeatAck[3] = now

	idx, err := rf.ReadIndex(context.Background())
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected commit index 1, got %d", idx)
	}
	if appendCalls.Load() != 0 {
		t.Fatalf("expected lease fast-path without heartbeats, got %d AppendEntries calls", appendCalls.Load())
	}
}

func TestReadIndexFallsBackWhenLeaseStale(t *testing.T) {
	rf := newTestRaft()
	rf.id = 1
	rf.state = Leader
	rf.currentTerm = 2
	rf.log = []LogEntry{{Term: 0}, {Term: 2, Command: nil}}
	rf.commitIndex = 1
	rf.nextIndex[2] = 2
	rf.nextIndex[3] = 2

	var appendCalls atomic.Int32
	mockFn := func(_ *AppendEntriesArgs, reply *AppendEntriesReply) error {
		appendCalls.Add(1)
		reply.Success = true
		reply.Term = 2
		return nil
	}
	rf.peers = map[NodeID]*Peer{
		2: &Peer{Client: &raftServiceMock{appendFn: mockFn}, Role: Voter},
		3: &Peer{Client: &raftServiceMock{appendFn: mockFn}, Role: Voter},
	}

	stale := time.Now().Add(-2 * rf.cfg.LeaderQuorumLivenessTimeout)
	rf.lastHeartbeatAck[2] = stale
	rf.lastHeartbeatAck[3] = stale

	idx, err := rf.ReadIndex(context.Background())
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected commit index 1, got %d", idx)
	}
	if appendCalls.Load() == 0 {
		t.Fatal("expected quorum heartbeat probe when lease is stale")
	}
}
