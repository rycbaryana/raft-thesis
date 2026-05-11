package raft

import (
	"log/slog"
	"time"
)

func (rf *Raft) run() {

	electionCheckTicker := time.NewTicker(50 * time.Millisecond)
	defer electionCheckTicker.Stop()
	rf.heartbeatTimer = time.NewTicker(rf.cfg.HeartbeatInterval)
	defer rf.heartbeatTimer.Stop()

	for {
		select {
		case <-rf.stopCh:
			return

		case <-electionCheckTicker.C:
			rf.mu.Lock()
			if rf.state == Leader {
				rf.maybeStepDownLeader()
			} else if rf.shouldStartElection() {
				rf.startElection()
			}
			rf.mu.Unlock()

		case <-rf.heartbeatTimer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				rf.broadcastAppendEntries()
			}
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) maybeStepDownLeader() {
	if rf.state != Leader {
		return
	}

	window := rf.cfg.LeaderQuorumLivenessTimeout
	now := time.Now()
	live := 0
	if rf.isVoter(rf.id) {
		live = 1
	}
	for peerId := range rf.voterMembers {
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
