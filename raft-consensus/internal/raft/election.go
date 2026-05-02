package raft

import (
	"log/slog"
	"time"
)

func (rf *Raft) startElection() {
	rf.becomeCandidate()

	term := rf.currentTerm
	lastLogIndex, lastLogTerm := rf.getLastLogInfo()

	rf.logf(slog.LevelInfo, "Starting election")

	votesReceived := 1
	quorumVotes := rf.quorumVotes()

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

			replyCh := make(chan struct {
				reply RequestVoteReply
				err   error
			}, 1)

			go func() {
				var reply RequestVoteReply
				err := peer.RequestVote(args, &reply)
				replyCh <- struct {
					reply RequestVoteReply
					err   error
				}{reply, err}
			}()

			var reply RequestVoteReply
			select {
			case res := <-replyCh:
				if res.err != nil {
					rf.mu.Lock()
					rf.logf(slog.LevelWarn, "Error on RequestVote to %d: %v", peerId, res.err)
					rf.mu.Unlock()
					return
				}
				reply = res.reply
			case <-time.After(2 * rf.cfg.HeartbeatInterval):
				rf.mu.Lock()
				rf.logf(slog.LevelWarn, "Timeout on RequestVote to %d", peerId)
				rf.mu.Unlock()
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
				if votesReceived == quorumVotes {
					rf.becomeLeader()
				}
			}
		}(peerId, peer)
	}
}

func (rf *Raft) resetElectionTimer() {
	rf.lastActivity = time.Now()
	rf.electionTimeout = getRandomDuration(rf.cfg.MinElectionTimeout, rf.cfg.MaxElectionTimeout)
}
