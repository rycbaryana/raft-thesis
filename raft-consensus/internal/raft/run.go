package raft

import (
	"time"
)

func (rf *Raft) run() {
	rf.resetElectionTimer()

	rf.heartbeatTimer = time.NewTicker(heartbeatTime)

	for {
		select {
		case <-rf.stopCh:
			return

		case <-rf.electionTimer.C:
			rf.mu.Lock()
			if rf.state != Leader {
				rf.startElection()
			} else {
				rf.resetElectionTimer()
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
