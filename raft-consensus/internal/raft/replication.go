package raft

import (
	"log"
)

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// log.Printf("[Node %d Term %d] Got AppendEntries from Node %d Term %d", rf.id, rf.currentTerm, args.LeaderID, args.Term)
	// 1. Term check
	if args.Term < rf.currentTerm {
		reply.Success = false
		reply.Term = rf.currentTerm
		return nil
	}

	// 2. Back to Follower if valid leader contacts us
	rf.resetElectionTimer()
	if args.Term > rf.currentTerm || rf.state != Follower {
		rf.becomeFollower(args.Term)
	}
	reply.Term = rf.currentTerm

	// 3. Log Consistency Check
	// Does our log have entry at PrevLogIndex with PrevLogTerm?
	lastLogIndex, _ := rf.getLastLogInfo()

	if args.PrevLogIndex > lastLogIndex {
		reply.Success = false
		return nil
	}

	if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.Success = false
		return nil
	}

	// 4. Append New Entries
	for i, entry := range args.Entries {
		index := args.PrevLogIndex + 1 + LogIndex(i)
		logSize := LogIndex(len(rf.log))

		if index < logSize && rf.log[index].Term != entry.Term {
			rf.log = rf.log[:index]
		}
		rf.log = append(rf.log, entry)
	}

	// 5. Update Commit Index
	if args.LeaderCommit > rf.commitIndex {
		lastNewIndex := args.PrevLogIndex + LogIndex(len(args.Entries))
		rf.commitIndex = min(args.LeaderCommit, lastNewIndex)

		go rf.applyLogs()
	}

	reply.Success = true
	return nil
}

func (rf *Raft) sendHeartbeat(peerId NodeID, peer RaftService) {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}

	nextIdx := rf.nextIndex[peerId]
	var entries []LogEntry

	lastLogIdx, _ := rf.getLastLogInfo()
	if nextIdx <= lastLogIdx {
		entries = rf.log[nextIdx:]
	} else {
		entries = []LogEntry{}
	}

	prevLogIndex := nextIdx - 1
	prevLogTerm := rf.log[prevLogIndex].Term

	args := &AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderID:     rf.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
	rf.mu.Unlock()

	var reply AppendEntriesReply

	err := peer.AppendEntries(args, &reply)
	if err != nil {
		log.Printf("[Node %d to Node %d] Error on AppendEntries(%d, %d, %d, %d, %v, %d): %v", rf.id, peerId, rf.currentTerm, rf.id, prevLogIndex, prevLogTerm, entries, rf.commitIndex, err)
		return
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != Leader {
		return
	}

	if reply.Term > rf.currentTerm {
		rf.becomeFollower(reply.Term)
		return
	}

	if reply.Success {
		newMatchIndex := args.PrevLogIndex + LogIndex(len(args.Entries))
		if newMatchIndex > rf.matchIndex[peerId] {
			rf.matchIndex[peerId] = newMatchIndex
			rf.nextIndex[peerId] = rf.matchIndex[peerId] + 1

			rf.updateCommitIndex()
		}
	} else {
		if rf.nextIndex[peerId] > 1 {
			rf.nextIndex[peerId]--
		}
	}

}

func (rf *Raft) broadcastHeartbeats() {
	for peerId, peer := range rf.peers {
		if peerId == rf.id {
			continue
		}
		go rf.sendHeartbeat(peerId, peer)
	}
}

func (rf *Raft) updateCommitIndex() {
	start := rf.commitIndex + 1
	last, _ := rf.getLastLogInfo()

	quorum := len(rf.peers) / 2 + 1;

	for n := start; n <= last; n++ {
		if rf.log[n].Term != rf.currentTerm {
			continue
		}

		count := 1 // self
		for peerId := range rf.peers {
			if peerId == rf.id {
				continue
			}
			if rf.matchIndex[peerId] >= n {
				count++
			}
		}

		if count >= quorum {
			rf.commitIndex = n
			log.Printf("Node %d committed index %d", rf.id, n)
		}
	}

	if rf.commitIndex > rf.lastApplied {
		go rf.applyLogs()
	}
}


func (rf *Raft) applyLogs() {
	rf.mu.Lock()
	
	baseIndex := rf.lastApplied
	commitIndex := rf.commitIndex
	
	if baseIndex >= commitIndex {
		rf.mu.Unlock()
		return
	}

	entries := make([]LogEntry, commitIndex - baseIndex)
	copy(entries, rf.log[baseIndex+1 : commitIndex+1])
	
	rf.lastApplied = commitIndex
	
	rf.mu.Unlock()

	for i, entry := range entries {
		rf.ApplyCh <- ApplyMsg{
			CommandValid: true,
			Command:      entry.Command,
			CommandIndex: baseIndex + 1 + LogIndex(i),
		}
	}
}