package raft

import (
	"log/slog"
	"time"
)

func (rf *Raft) becomeFollower(term Term) {
	stateBefore := rf.state
	rf.state = Follower
	rf.currentTerm = term
	rf.votedFor = NoNode
	rf.resetElectionTimer()

	if stateBefore != Follower {
		rf.logf(slog.LevelInfo, "State change: %s -> Follower", stateBefore)
	}
}

func (rf *Raft) becomeCandidate() {
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.id
	rf.resetElectionTimer()

	rf.logf(slog.LevelInfo, "State change: Follower -> Candidate")
}

func (rf *Raft) becomeLeader() {
	if rf.state == Leader {
		return
	}

	rf.state = Leader
	rf.hintLeaderID = rf.id
	rf.logf(slog.LevelInfo, "State change: Candidate -> LEADER")

	lastIndex, _ := rf.getLastLogInfo()
	now := time.Now()
	for peerId := range rf.peers {
		rf.nextIndex[peerId] = lastIndex + 1
		rf.matchIndex[peerId] = 0
		if peerId != rf.id {
			rf.lastHeartbeatAck[peerId] = now
		}
	}

	rf.log = append(rf.log, LogEntry{Term: rf.currentTerm, Command: nil}) // no-op entry to commit entries from previous term

	rf.broadcastReplication()
}
