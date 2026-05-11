package raft

import (
	"log/slog"
	"slices"
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

	lastIdx := LogIndex(len(rf.log) - 1)
	if lastIdx == 0 && len(rf.bootstrapCluster) > 0 {
		for _, id := range sortedBootstrapIDs(rf.bootstrapCluster) {
			addr := rf.bootstrapCluster[id]
			cmd, err := encodeConfigAdd(id, addr)
			if err != nil {
				rf.logf(slog.LevelWarn, "founding cluster encode for node %d: %v", id, err)
				continue
			}
			rf.log = append(rf.log, LogEntry{
				Type:    EntryAddVoter,
				Term:    rf.currentTerm,
				Command: cmd,
			})
		}
		rf.logf(slog.LevelInfo, "appended founding cluster (%d voters) to replicated log", len(rf.bootstrapCluster))
	}

	lastIndex, _ := rf.getLastLogInfo()
	now := time.Now()
	for peerId := range rf.peers {
		if peerId == rf.id {
			continue
		}
		rf.nextIndex[peerId] = lastIndex + 1
		rf.matchIndex[peerId] = 0
		rf.lastHeartbeatAck[peerId] = now
	}

	rf.log = append(rf.log, LogEntry{Type: EntryNormal, Term: rf.currentTerm, Command: nil}) // no-op entry to commit entries from previous term

	rf.broadcastAppendEntries()
}

func sortedBootstrapIDs(m map[NodeID]string) []NodeID {
	ids := make([]NodeID, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
