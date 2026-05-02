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

	// Infrastructure
	id    NodeID
	fsm   StateMachine
	peers map[NodeID]RaftService
	cfg   Config

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

// WithConfig replaces timing config. Zero fields in c are filled from DefaultConfig().
func WithConfig(c Config) Option {
	return func(rf *Raft) {
		rf.cfg = c
	}
}

func NewRaft(id NodeID, peers map[NodeID]RaftService, fsm StateMachine, opts ...Option) *Raft {
	rf := &Raft{
		id:               id,
		fsm:              fsm,
		peers:            peers,
		logger:           slog.Default(),
		state:            Follower,
		votedFor:         NoNode,
		hintLeaderID:     NoNode,
		log:              make([]LogEntry, 0),
		nextIndex:        make(map[NodeID]LogIndex),
		matchIndex:       make(map[NodeID]LogIndex),
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

	rf.resetElectionTimer()

	go rf.run()
	go rf.applier()

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
		Term:    rf.currentTerm,
		Command: command,
	}

	rf.log = append(rf.log, entry)
	index := LogIndex(len(rf.log) - 1)

	rf.broadcastReplication()

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
	close(rf.stopCh)
}
