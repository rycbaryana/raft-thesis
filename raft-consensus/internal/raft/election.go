package raft

import (
	"log"
)

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	log.Printf("[Node %d Term %d] Got RequestVote from Node %d Term %d", rf.id, rf.currentTerm, args.CandidateID, args.Term)
	// 1. Term check
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		return nil
	}

	// 2. Step down if fresher term
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
	}
	reply.Term = rf.currentTerm

	// 3. Check if already voted
	if rf.votedFor != NoNode && rf.votedFor != args.CandidateID {
		reply.VoteGranted = false
		return nil
	}

	// 4. Log should be up-to-date
	lastLogIndex, lastLogTerm := rf.getLastLogInfo()

	isLogUpToDate := false
	if args.LastLogTerm > lastLogTerm {
		isLogUpToDate = true
	} else if args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex {
		isLogUpToDate = true
	}

	if isLogUpToDate {
		rf.votedFor = args.CandidateID
		rf.resetElectionTimer()
		reply.VoteGranted = true
		log.Printf("[Node %d] Voted FOR %d in term %d", rf.id, args.CandidateID, rf.currentTerm)
	} else {
		reply.VoteGranted = false
	}

	return nil
}

func (rf *Raft) startElection() {
	rf.becomeCandidate()

	term := rf.currentTerm
	lastLogIndex, lastLogTerm := rf.getLastLogInfo()

	// log.Printf("[Node %d] Starting election for term %d", rf.id, term)

	votesReceived := 1
	votesNeeded := len(rf.peers)/2 + 1

	for peerId, peer := range rf.peers {
		if peerId == rf.id {
			continue
		}

		go func(peerId NodeID, peer RaftService) {
			args := &RequestVoteArgs{
				Term:         term,
				CandidateID:  rf.id,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
			}
			var reply RequestVoteReply

			err := peer.RequestVote(args, &reply)
			if err != nil {
				log.Printf("[Node %d to Node %d] Error on RequestVote(%d, %d, %d, %d): %v", rf.id, peerId, term, rf.id ,lastLogIndex, lastLogTerm, err)
				return
			}

			rf.mu.Lock()
			defer rf.mu.Unlock()

			if rf.state != Candidate || rf.currentTerm != term {
				return
			}

			if reply.Term > term {
				rf.becomeFollower(reply.Term)
				return
			}

			if reply.VoteGranted {
				votesReceived++
				if votesReceived == votesNeeded {
					rf.becomeLeader()
				}
			}
		}(peerId, peer)
	}
}

func (rf *Raft) resetElectionTimer() {
	if rf.electionTimer != nil {
		rf.electionTimer.Stop()
	}
	timeout := getRandomDuration(minElectionTimeout, maxElectionTimeout)
	rf.electionTimer.Reset(timeout)
}
