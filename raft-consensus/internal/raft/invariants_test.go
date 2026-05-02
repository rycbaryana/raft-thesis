package raft

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type raftServiceMock struct {
	appendFn func(args *AppendEntriesArgs, reply *AppendEntriesReply) error
}

type recordingFSM struct {
	mu   sync.Mutex
	seen []string
}

func (f *recordingFSM) Apply(command []byte) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, string(command))
	return nil, nil
}

func (m *raftServiceMock) RequestVote(_ *RequestVoteArgs, _ *RequestVoteReply) error {
	return nil
}

func (m *raftServiceMock) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	return m.appendFn(args, reply)
}

func newTestRaft() *Raft {
	rf := &Raft{
		id:               1,
		peers:            map[NodeID]RaftService{},
		log:              []LogEntry{{Term: 0}},
		nextIndex:        map[NodeID]LogIndex{},
		matchIndex:       map[NodeID]LogIndex{},
		lastHeartbeatAck: make(map[NodeID]time.Time),
		votedFor:         NoNode,
		state:            Follower,
		applyFutures:     map[LogIndex]*indexFuture{},
		readTokens:       map[uint64]struct{}{},
		cfg:              DefaultTestingConfig(),
	}
	rf.cfg = rf.cfg.normalize()
	rf.applyCond = sync.NewCond(&rf.mu)
	return rf
}

func TestAppendEntriesHigherTermUpdatesFollowerTerm(t *testing.T) {
	rf := newTestRaft()
	rf.currentTerm = 2
	rf.state = Follower

	args := &AppendEntriesArgs{
		Term:         3,
		LeaderID:     2,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
	}
	reply := &AppendEntriesReply{}
	if err := rf.AppendEntries(args, reply); err != nil {
		t.Fatalf("append entries failed: %v", err)
	}
	if rf.currentTerm != 3 {
		t.Fatalf("expected term=3, got %d", rf.currentTerm)
	}
}

func TestUpdateCommitIndexCommitsOnlyCurrentTermByCounting(t *testing.T) {
	rf := newTestRaft()
	rf.state = Leader
	rf.currentTerm = 3
	rf.peers = map[NodeID]RaftService{
		2: nil,
		3: nil,
	}
	rf.log = []LogEntry{
		{Term: 0},
		{Term: 1},
		{Term: 2},
		{Term: 3},
	}
	rf.commitIndex = 1
	rf.matchIndex[2] = 2
	rf.matchIndex[3] = 2

	rf.updateCommitIndex()
	if rf.commitIndex != 1 {
		t.Fatalf("expected commit index to stay at 1, got %d", rf.commitIndex)
	}

	rf.matchIndex[2] = 3
	rf.updateCommitIndex()
	if rf.commitIndex != 3 {
		t.Fatalf("expected commit index to advance to 3, got %d", rf.commitIndex)
	}
}

func TestApplierMaintainsApplyOrder(t *testing.T) {
	rf := newTestRaft()
	fsm := &recordingFSM{}
	rf.fsm = fsm
	rf.log = []LogEntry{
		{Term: 0},
		{Term: 1, Command: []byte("a")},
		{Term: 1, Command: []byte("b")},
		{Term: 1, Command: []byte("c")},
	}
	rf.commitIndex = 3

	go rf.applier()

	deadline := time.After(500 * time.Millisecond)
	for {
		fsm.mu.Lock()
		done := len(fsm.seen) == 3
		fsm.mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for fsm applies")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	fsm.mu.Lock()
	defer fsm.mu.Unlock()
	if len(fsm.seen) != 3 {
		t.Fatalf("expected 3 applied commands, got %d", len(fsm.seen))
	}
	if !slices.Equal(fsm.seen, []string{"a", "b", "c"}) {
		t.Fatalf("unexpected apply order: %#v", fsm.seen)
	}
}

func TestLeaderStepsDownOnHigherTermAppendReply(t *testing.T) {
	rf := newTestRaft()
	rf.state = Leader
	rf.currentTerm = 4
	rf.log = []LogEntry{{Term: 0}, {Term: 4, Command: []byte("x")}}
	rf.nextIndex[2] = 1
	rf.peers[2] = &raftServiceMock{
		appendFn: func(_ *AppendEntriesArgs, reply *AppendEntriesReply) error {
			reply.Success = false
			reply.Term = 5
			return nil
		},
	}

	ok := rf.sendReplication(2, rf.peers[2])
	if ok {
		t.Fatalf("expected replication failure on higher term reply")
	}
	if rf.state != Follower {
		t.Fatalf("expected step down to follower, got %s", rf.state.String())
	}
	if rf.currentTerm != 5 {
		t.Fatalf("expected currentTerm=5, got %d", rf.currentTerm)
	}
}

func TestSendHeartbeatReturnsFalseOnConflict(t *testing.T) {
	rf := newTestRaft()
	rf.state = Leader
	rf.currentTerm = 1
	rf.log = []LogEntry{{Term: 0}, {Term: 1}}
	rf.nextIndex[2] = 2
	rf.commitIndex = 1
	rf.lastHeartbeatAck[2] = time.Now().Add(-24 * time.Hour)
	stale := rf.lastHeartbeatAck[2]

	rf.peers[2] = &raftServiceMock{
		appendFn: func(_ *AppendEntriesArgs, reply *AppendEntriesReply) error {
			reply.Success = false
			reply.ConflictIndex = 1
			reply.ConflictTerm = 0
			reply.Term = 1
			return nil
		},
	}

	if rf.sendHeartbeat(2, rf.peers[2]) {
		t.Fatalf("expected heartbeat failure on conflict")
	}
	if rf.lastHeartbeatAck[2] != stale {
		t.Fatalf("heartbeat conflict must not refresh lastHeartbeatAck")
	}
}

func TestLeaderStepsDownWhenHeartbeatQuorumLost(t *testing.T) {
	rf := newTestRaft()
	rf.id = 1
	rf.state = Leader
	rf.currentTerm = 2
	rf.log = []LogEntry{{Term: 0}, {Term: 2}}
	rf.commitIndex = 1
	rf.nextIndex[2] = 2
	rf.nextIndex[3] = 2
	rf.peers = map[NodeID]RaftService{2: nil, 3: nil}
	rf.cfg.LeaderQuorumLivenessTimeout = time.Minute
	rf.lastHeartbeatAck[2] = time.Now().Add(-24 * time.Hour)
	rf.lastHeartbeatAck[3] = time.Now().Add(-24 * time.Hour)

	rf.mu.Lock()
	rf.maybeStepDownLeader()
	rf.mu.Unlock()

	if rf.state != Follower {
		t.Fatalf("expected step down to follower on heartbeat quorum loss, got %s", rf.state.String())
	}
	if rf.currentTerm != 2 {
		t.Fatalf("expected same term after voluntary step down, got %d", rf.currentTerm)
	}
}

func TestReadIndexSendsOnlyHeartbeats(t *testing.T) {
	rf := newTestRaft()
	rf.id = 1
	rf.state = Leader
	rf.currentTerm = 2
	rf.log = []LogEntry{{Term: 0}, {Term: 2, Command: nil}}
	rf.commitIndex = 1
	rf.nextIndex[2] = 2
	rf.nextIndex[3] = 2

	var sawNonEmpty atomic.Bool
	mockFn := func(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
		if len(args.Entries) > 0 {
			sawNonEmpty.Store(true)
		}
		reply.Success = true
		reply.Term = 2
		return nil
	}
	rf.peers = map[NodeID]RaftService{
		2: &raftServiceMock{appendFn: mockFn},
		3: &raftServiceMock{appendFn: mockFn},
	}

	idx, err := rf.ReadIndex(context.Background())
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	if idx != 1 {
		t.Fatalf("expected commit index 1, got %d", idx)
	}
	if sawNonEmpty.Load() {
		t.Fatalf("ReadIndex must not send non-empty AppendEntries")
	}
}
