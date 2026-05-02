package raft

import (
	"log/slog"
	"time"
)

func (rf *Raft) quorumLivenessWindow() time.Duration {
	return rf.cfg.LeaderQuorumLivenessTimeout
}

// sendHeartbeat sends an empty AppendEntries to peerId. Returns true iff reply.Success.
func (rf *Raft) sendHeartbeat(peerId NodeID, peer RaftService) bool {
	return rf.leaderSendAppendEntries(peerId, peer, true)
}

func (rf *Raft) broadcastHeartbeats() {
	for peerId, peer := range rf.peers {
		if peerId == rf.id {
			continue
		}
		go rf.sendHeartbeat(peerId, peer)
	}
}

// maybeStepDownLeader becomes Follower if a quorum of recent heartbeat acks is not met.
// Caller must hold rf.mu.
func (rf *Raft) maybeStepDownLeader() {
	if rf.state != Leader {
		return
	}

	window := rf.quorumLivenessWindow()
	now := time.Now()
	live := 1 // self
	for peerId := range rf.peers {
		if peerId == rf.id {
			continue
		}
		if t, ok := rf.lastHeartbeatAck[peerId]; ok && now.Sub(t) < window {
			live++
		}
	}

	if live < rf.quorumVotes() {
		rf.logf(slog.LevelInfo, "Stepping down: heartbeat quorum lost (live=%d need=%d)", live, rf.quorumVotes())
		rf.becomeFollower(rf.currentTerm)
	}
}
