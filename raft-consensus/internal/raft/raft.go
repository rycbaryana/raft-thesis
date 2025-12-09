package raft

import (
	"sync"
	"time"
)

var _ RaftService = (*Raft)(nil)

type Raft struct {
	mu sync.Mutex

	// Infrastructure
	id    NodeID
	peers map[NodeID]RaftService

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

	// Timers
	electionTimer  *time.Timer
	heartbeatTimer *time.Ticker

	// Control
	stopCh  chan struct{}
	ApplyCh chan ApplyMsg
}

func NewRaft(id NodeID, peers map[NodeID]RaftService, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		id:         id,
		peers:      peers,
		state:      Follower,
		votedFor:   NoNode,
		log:        make([]LogEntry, 0),
		nextIndex:  make(map[NodeID]LogIndex),
		matchIndex: make(map[NodeID]LogIndex),
		stopCh:     make(chan struct{}),
		ApplyCh:    applyCh,
	}

	rf.log = append(rf.log, LogEntry{Term: 0})

	rf.electionTimer = time.NewTimer(10 * time.Second)

	go rf.run()

	return rf
}

func (rf *Raft) Submit(command any) (bool, LogIndex, Term) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != Leader {
		return false, 0, 0
	}

	entry := LogEntry{
		Term:    rf.currentTerm,
		Command: command,
	}

	rf.log = append(rf.log, entry)
	index := LogIndex(len(rf.log) - 1)

	rf.broadcastHeartbeats()

	return true, index, rf.currentTerm
}
