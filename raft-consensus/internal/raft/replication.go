package raft

import (
	"log/slog"
	"time"
)

func (rf *Raft) sendAppendEntries(peerId NodeID) bool {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return false
	}

	p := rf.peers[peerId]
	if p == nil || p.Client == nil {
		rf.mu.Unlock()
		return false
	}
	if rf.appendInFlight == nil {
		rf.appendInFlight = make(map[NodeID]bool)
	}
	if rf.appendInFlight[peerId] {
		rf.mu.Unlock()
		return false
	}
	rf.appendInFlight[peerId] = true

	nextIdx := rf.nextIndex[peerId]
	lastLogIdx, _ := rf.getLastLogInfo()

	var entries []LogEntry
	if nextIdx <= lastLogIdx {
		entries = rf.log[nextIdx:]
	} else {
		entries = nil
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
	client := p.Client
	rf.mu.Unlock()
	defer func() {
		rf.mu.Lock()
		rf.appendInFlight[peerId] = false
		rf.mu.Unlock()
	}()

	replyCh := make(chan struct {
		reply AppendEntriesReply
		err   error
	}, 1)

	go func() {
		var reply AppendEntriesReply
		err := client.AppendEntries(args, &reply)
		replyCh <- struct {
			reply AppendEntriesReply
			err   error
		}{reply, err}
	}()

	var reply AppendEntriesReply
	select {
	case res := <-replyCh:
		if res.err != nil {
			rf.mu.Lock()
			rf.logf(slog.LevelWarn, "Error on AppendEntries to %d: %v", peerId, res.err)
			rf.mu.Unlock()
			return false
		}
		reply = res.reply
	case <-time.After(rf.cfg.RPCTimeout):
		rf.mu.Lock()
		rf.logf(slog.LevelWarn, "Timeout on AppendEntries to %d", peerId)
		rf.mu.Unlock()
		return false
	}

	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return false
	}

	if !reply.Success || len(args.Entries) > 0 || reply.Term > rf.currentTerm {
		rf.logf(slog.LevelDebug, "AppendEntries response from %d: success=%v term=%d conflictIndex=%d conflictTerm=%d (prevIdx=%d prevTerm=%d entries=%d)",
			peerId, reply.Success, reply.Term, reply.ConflictIndex, reply.ConflictTerm,
			args.PrevLogIndex, args.PrevLogTerm, len(args.Entries))
	}

	if reply.Term > rf.currentTerm {
		rf.becomeFollower(reply.Term)
		rf.mu.Unlock()
		return false
	}

	if reply.Success {
		newMatchIndex := args.PrevLogIndex + LogIndex(len(args.Entries))
		if newMatchIndex > rf.matchIndex[peerId] {
			rf.matchIndex[peerId] = newMatchIndex
			rf.nextIndex[peerId] = newMatchIndex + 1

			rf.updateCommitIndex()
		}
		rf.lastHeartbeatAck[peerId] = time.Now()
		rf.mu.Unlock()
		return true
	}

	if reply.ConflictTerm == 0 {
		rf.nextIndex[peerId] = reply.ConflictIndex
	} else {
		lastLogIndex, _ := rf.getLastLogInfo()
		found := false
		for i := lastLogIndex; i >= 1; i-- {
			if rf.log[i].Term == reply.ConflictTerm {
				rf.nextIndex[peerId] = i + 1
				found = true
				break
			}
			if rf.log[i].Term < reply.ConflictTerm {
				break
			}
		}
		if !found {
			rf.nextIndex[peerId] = reply.ConflictIndex
		}
	}
	if rf.nextIndex[peerId] < 1 {
		rf.nextIndex[peerId] = 1
	}
	rf.mu.Unlock()
	return false
}

func (rf *Raft) broadcastAppendEntries() {
	for peerId, peer := range rf.peers {
		if peerId == rf.id || peer == nil || peer.Client == nil {
			continue
		}
		go rf.sendAppendEntries(peerId)
	}
}

func (rf *Raft) updateCommitIndex() {
	start := rf.commitIndex + 1
	last, _ := rf.getLastLogInfo()

	quorum := rf.quorumVotes()

	for n := start; n <= last; n++ {
		if rf.log[n].Term != rf.currentTerm {
			continue
		}

		count := 0
		if rf.isVoter(rf.id) {
			count++
		}
		for peerId := range rf.voterMembers {
			if peerId == rf.id {
				continue
			}
			if rf.matchIndex[peerId] >= n {
				count++
			}
		}

		if count >= quorum {
			rf.commitIndex = n
			rf.logf(slog.LevelInfo, "Committed index %d", n)
		}
	}

	if rf.commitIndex > rf.lastApplied {
		rf.applyCond.Broadcast()
	}
}

func (rf *Raft) applier() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	for {
		for rf.commitIndex <= rf.lastApplied {
			select {
			case <-rf.stopCh:
				return
			default:
			}
			rf.applyCond.Wait()
		}

		baseIndex := rf.lastApplied
		commitIndex := rf.commitIndex

		entries := make([]LogEntry, commitIndex-baseIndex)
		copy(entries, rf.log[baseIndex+1:commitIndex+1])

		rf.mu.Unlock()

		for i, entry := range entries {
			index := baseIndex + 1 + LogIndex(i)
			var applyErr error
			switch entry.Type {
			case EntryAddLearner, EntryAddVoter, EntryRemoveNode:
				continue
			default:
				if rf.fsm != nil && entry.Command != nil {
					rf.logf(slog.LevelDebug, "Applying command %d: %s", index, string(entry.Command))
					_, applyErr = rf.fsm.Apply(entry.Command)
					if applyErr != nil {
						rf.mu.Lock()
						rf.getFuture(index).err = applyErr
						rf.mu.Unlock()
					}
				}
			}
		}

		rf.mu.Lock()
		if commitIndex > rf.lastApplied {
			rf.lastApplied = commitIndex
			rf.resolveFutures(rf.lastApplied)
		}
	}
}
