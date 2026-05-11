package raft

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

var _ RaftService = (*Raft)(nil)

type Raft struct {
	mu sync.Mutex
	// readIndexMu serializes ReadIndex quorum rounds to avoid self-induced
	// no-quorum failures under many concurrent linearizable reads.
	readIndexMu sync.Mutex

	// Infrastructure
	id               NodeID
	fsm              StateMachine
	peers            map[NodeID]*Peer
	peerFactory      PeerFactory
	bootstrapCluster map[NodeID]string
	activeMembers    map[NodeID]struct{}
	voterMembers     map[NodeID]struct{}
	memberAddrs      map[NodeID]string
	cfg              Config

	// Persistent state
	currentTerm Term
	votedFor    NodeID
	log         []LogEntry

	// Volatile state
	commitIndex LogIndex
	lastApplied LogIndex
	state       State

	// Leader Volatile state
	nextIndex  map[NodeID]LogIndex
	matchIndex map[NodeID]LogIndex
	// appendInFlight gates replication pressure: at most one AppendEntries RPC per peer at a time.
	appendInFlight map[NodeID]bool

	// lastHeartbeatAck records the last time a follower acked an empty AppendEntries (heartbeat).
	lastHeartbeatAck map[NodeID]time.Time

	// Last known leader (AppendEntries); for client redirects when not leader.
	hintLeaderID NodeID

	// Timers
	lastActivity    time.Time
	electionTimeout time.Duration
	heartbeatTimer  *time.Ticker

	// Control
	applyCond *sync.Cond
	stopCh    chan struct{}
	stopOnce  sync.Once
	runWG     sync.WaitGroup

	applyFutures map[LogIndex]*indexFuture
	readTokenSeq uint64
	readTokens   map[uint64]struct{}

	logger *slog.Logger
}

type Option func(*Raft)

func WithLogger(l *slog.Logger) Option {
	return func(rf *Raft) {
		rf.logger = l
	}
}

func WithConfig(c Config) Option {
	return func(rf *Raft) {
		rf.cfg = c
	}
}

func NewRaft(id NodeID, fsm StateMachine, factory PeerFactory, initialCluster map[NodeID]string, opts ...Option) *Raft {
	bootstrap := make(map[NodeID]string, len(initialCluster))
	for k, v := range initialCluster {
		bootstrap[k] = v
	}

	rf := &Raft{
		id:               id,
		fsm:              fsm,
		peerFactory:      factory,
		bootstrapCluster: bootstrap,
		activeMembers:    make(map[NodeID]struct{}),
		memberAddrs:      make(map[NodeID]string),
		peers:            make(map[NodeID]*Peer),
		logger:           slog.Default(),
		state:            Follower,
		votedFor:         NoNode,
		hintLeaderID:     NoNode,
		log:              make([]LogEntry, 0),
		nextIndex:        make(map[NodeID]LogIndex),
		matchIndex:       make(map[NodeID]LogIndex),
		appendInFlight:   make(map[NodeID]bool),
		lastHeartbeatAck: make(map[NodeID]time.Time),
		stopCh:           make(chan struct{}),
		applyFutures:     make(map[LogIndex]*indexFuture),
		readTokens:       make(map[uint64]struct{}),
	}

	rf.applyCond = sync.NewCond(&rf.mu)

	for _, opt := range opts {
		opt(rf)
	}

	rf.cfg = rf.cfg.normalize()

	rf.log = append(rf.log, LogEntry{Term: 0})

	rf.rebuildVolatilePeers(LogIndex(len(rf.log) - 1))

	rf.resetElectionTimer()

	rf.runWG.Add(2)
	go func() {
		defer rf.runWG.Done()
		rf.run()
	}()
	go func() {
		defer rf.runWG.Done()
		rf.applier()
	}()

	return rf
}

func (rf *Raft) Submit(command []byte) (bool, LogIndex, Term) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.logf(slog.LevelDebug, "Submit: %s", string(command))

	if rf.state != Leader {
		return false, 0, 0
	}

	entry := LogEntry{
		Type:    EntryNormal,
		Term:    rf.currentTerm,
		Command: command,
	}

	rf.log = append(rf.log, entry)
	index := LogIndex(len(rf.log) - 1)

	rf.broadcastAppendEntries()

	return true, index, rf.currentTerm
}

func (rf *Raft) SubmitAndWait(ctx context.Context, command []byte) (bool, LogIndex, Term, error) {
	isLeader, index, term := rf.Submit(command)
	if !isLeader {
		return false, 0, 0, ErrNotLeader
	}
	if err := rf.waitForApplied(ctx, index); err != nil {
		return true, index, term, err
	}
	return true, index, term, nil
}

func (rf *Raft) AddNode(id NodeID, addr string) error {
	cmd, err := encodeConfigAdd(id, addr)
	if err != nil {
		return err
	}

	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return ErrNotLeader
	}
	if rf.hasPendingAddVoterOrRemove() {
		rf.mu.Unlock()
		return ErrConfigChangeInProgress
	}
	if rf.isMember(id) {
		rf.mu.Unlock()
		return ErrPeerAlreadyInCluster
	}

	entry := LogEntry{
		Type:    EntryAddLearner,
		Term:    rf.currentTerm,
		Command: cmd,
	}
	rf.log = append(rf.log, entry)
	index := LogIndex(len(rf.log) - 1)
	rf.rebuildVolatilePeers(index)
	rf.logf(slog.LevelInfo, "added node %d addr=%q as LEARNER (uncommitted) at index %d", id, addr, index)
	rf.broadcastAppendEntries()
	rf.startCatchUpPromoter(id)
	rf.mu.Unlock()
	return nil
}

func (rf *Raft) RemoveNode(id NodeID) error {
	cmd, err := encodeConfigRemove(id)
	if err != nil {
		return err
	}

	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return ErrNotLeader
	}
	if !rf.isMember(id) {
		rf.mu.Unlock()
		return ErrNodeNotInCluster
	}
	if rf.hasPendingMembershipCommit() {
		rf.mu.Unlock()
		return ErrConfigChangeInProgress
	}

	selfRemove := id == rf.id

	entry := LogEntry{
		Type:    EntryRemoveNode,
		Term:    rf.currentTerm,
		Command: cmd,
	}
	rf.log = append(rf.log, entry)
	index := LogIndex(len(rf.log) - 1)
	rf.rebuildVolatilePeers(index)
	rf.logf(slog.LevelInfo, "removed node %d from peers configuration (uncommitted) at index %d", id, index)
	rf.broadcastAppendEntries()
	rf.mu.Unlock()

	if err := rf.waitForApplied(context.Background(), index); err != nil {
		return err
	}

	if selfRemove {
		rf.mu.Lock()
		rf.becomeFollower(rf.currentTerm)
		rf.mu.Unlock()
		rf.logf(slog.LevelInfo, "stepped down after committed self-removal from cluster")
	}
	return nil
}

func (rf *Raft) LeaderHint() NodeID {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state == Leader {
		return rf.id
	}
	if rf.hintLeaderID != NoNode {
		return rf.hintLeaderID
	}
	return NoNode
}

func (rf *Raft) Stop() {
	rf.stopOnce.Do(func() {
		close(rf.stopCh)
		rf.mu.Lock()
		rf.applyCond.Broadcast()
		rf.mu.Unlock()
	})
}

func (rf *Raft) WaitStopped() {
	rf.runWG.Wait()
}
