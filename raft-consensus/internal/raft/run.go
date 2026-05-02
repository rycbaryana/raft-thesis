package raft

import (
	"time"
)

func (rf *Raft) run() {

	electionCheckTicker := time.NewTicker(50 * time.Millisecond)
	rf.heartbeatTimer = time.NewTicker(rf.cfg.HeartbeatInterval)

	for {
		select {
		case <-rf.stopCh:
			return

		case <-electionCheckTicker.C:
			rf.mu.Lock()
			if rf.state != Leader && time.Since(rf.lastActivity) >= rf.electionTimeout {
				rf.startElection()
			} else if rf.state == Leader {
				rf.maybeStepDownLeader()
			}
			rf.mu.Unlock()

		case <-rf.heartbeatTimer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				rf.broadcastHeartbeats()
			}
			rf.mu.Unlock()
		}
	}
}
