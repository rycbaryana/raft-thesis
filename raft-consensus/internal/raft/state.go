package raft

import (
	"log"
)

func (rf *Raft) becomeFollower(term Term) {
	stateBefore := rf.state
	rf.state = Follower
	rf.currentTerm = term
	rf.votedFor = NoNode
	rf.resetElectionTimer()
	
	if stateBefore != Follower {
		log.Printf("[Node %d] State change: %s -> Follower (Term %d)", rf.id, stateBefore, term)
	}
}

func (rf *Raft) becomeCandidate() {
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.id
	rf.resetElectionTimer()
	
	log.Printf("[Node %d] State change: Follower -> Candidate (Term %d)", rf.id, rf.currentTerm)
}

func (rf *Raft) becomeLeader() {
	if rf.state == Leader {
		return
	}
	
	rf.state = Leader
	log.Printf("[Node %d] State change: Candidate -> LEADER (Term %d)", rf.id, rf.currentTerm)

	lastIndex, _ := rf.getLastLogInfo()
	for peerId := range rf.peers {
		rf.nextIndex[peerId] = lastIndex + 1
		rf.matchIndex[peerId] = 0
	}

	rf.broadcastHeartbeats()
}
