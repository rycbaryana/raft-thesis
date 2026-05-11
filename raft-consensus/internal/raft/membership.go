package raft

import (
	"log/slog"
	"time"
)

func entryTypeChangesMembership(t EntryType) bool {
	return t == EntryAddLearner || t == EntryAddVoter || t == EntryRemoveNode
}

// rebuildVolatilePeers reapplies bootstrap + log[1:lastIndex] to activeMembers, voterMembers,
// memberAddrs, and rf.peers. Caller must hold rf.mu.
func (rf *Raft) rebuildVolatilePeers(lastIndex LogIndex) {
	members := make(map[NodeID]string, len(rf.bootstrapCluster))
	roles := make(map[NodeID]PeerRole, len(rf.bootstrapCluster))
	for id, addr := range rf.bootstrapCluster {
		members[id] = addr
		roles[id] = Voter
	}

	for i := LogIndex(1); i <= lastIndex && int(i) < len(rf.log); i++ {
		e := rf.log[i]
		switch e.Type {
		case EntryAddLearner:
			id, addr, err := decodeConfigAdd(e.Command)
			if err != nil {
				rf.logf(slog.LevelWarn, "config add learner at index %d: %v", i, err)
				continue
			}
			members[id] = addr
			roles[id] = Learner
			rf.logf(slog.LevelDebug, "membership replay: add learner %d at log index %d", id, i)
		case EntryAddVoter:
			id, addr, err := decodeConfigAdd(e.Command)
			if err != nil {
				rf.logf(slog.LevelWarn, "config add voter at index %d: %v", i, err)
				continue
			}
			members[id] = addr
			roles[id] = Voter
			rf.logf(slog.LevelDebug, "membership replay: promote %d to voter at log index %d", id, i)
		case EntryRemoveNode:
			id, err := decodeConfigRemove(e.Command)
			if err != nil {
				rf.logf(slog.LevelWarn, "config remove entry at index %d: %v", i, err)
				continue
			}
			delete(members, id)
			delete(roles, id)
			rf.logf(slog.LevelDebug, "membership replay: remove node %d at log index %d", id, i)
		}
	}

	prevAddrs := rf.memberAddrs
	if prevAddrs == nil {
		prevAddrs = map[NodeID]string{}
	}
	prevPeers := rf.peers

	rf.activeMembers = make(map[NodeID]struct{}, len(members))
	rf.voterMembers = make(map[NodeID]struct{}, len(members))
	rf.memberAddrs = make(map[NodeID]string, len(members))
	for id, addr := range members {
		rf.activeMembers[id] = struct{}{}
		rf.memberAddrs[id] = addr
		if roles[id] == Voter {
			rf.voterMembers[id] = struct{}{}
		}
	}

	newPeers := make(map[NodeID]*Peer, len(members))
	for id, addr := range members {
		if id == rf.id {
			continue
		}
		role := roles[id]
		if addr != "" {
			if prev, ok := prevAddrs[id]; ok && prev == addr {
				if p, ok2 := prevPeers[id]; ok2 && p != nil && p.Client != nil {
					newPeers[id] = &Peer{Client: p.Client, Role: role}
					continue
				}
			}
			var client RaftService
			if rf.peerFactory != nil {
				client = rf.peerFactory(id, addr)
			}
			newPeers[id] = &Peer{Client: client, Role: role}
			continue
		}
		if p, ok := prevPeers[id]; ok && p != nil {
			newPeers[id] = &Peer{Client: p.Client, Role: role}
		} else {
			newPeers[id] = &Peer{Client: nil, Role: role}
		}
	}

	rf.peers = newPeers

	lastLog, _ := rf.getLastLogInfo()
	for id := range rf.nextIndex {
		if _, ok := rf.activeMembers[id]; !ok || id == rf.id {
			delete(rf.nextIndex, id)
			delete(rf.matchIndex, id)
			delete(rf.lastHeartbeatAck, id)
		}
	}
	for id := range rf.activeMembers {
		if id == rf.id {
			continue
		}
		if _, ok := rf.nextIndex[id]; !ok {
			rf.nextIndex[id] = lastLog + 1
			rf.matchIndex[id] = 0
			rf.lastHeartbeatAck[id] = time.Now()
		}
	}
}

func (rf *Raft) hasPendingAddVoterOrRemove() bool {
	for i := rf.commitIndex + 1; i < LogIndex(len(rf.log)); i++ {
		switch rf.log[i].Type {
		case EntryAddVoter, EntryRemoveNode:
			return true
		}
	}
	return false
}

func (rf *Raft) hasPendingMembershipCommit() bool {
	for i := rf.commitIndex + 1; i < LogIndex(len(rf.log)); i++ {
		if entryTypeChangesMembership(rf.log[i].Type) {
			return true
		}
	}
	return false
}

func (rf *Raft) isMember(id NodeID) bool {
	_, ok := rf.activeMembers[id]
	return ok
}

func (rf *Raft) isVoter(id NodeID) bool {
	_, ok := rf.voterMembers[id]
	return ok
}

func (rf *Raft) startCatchUpPromoter(target NodeID) {
	go rf.runCatchUpPromoter(target)
}

func (rf *Raft) runCatchUpPromoter(target NodeID) {
	ticker := time.NewTicker(rf.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-rf.stopCh:
			return
		case <-ticker.C:
			rf.mu.Lock()
			if rf.state != Leader {
				rf.mu.Unlock()
				return
			}
			p, ok := rf.peers[target]
			if !ok || p == nil || p.Role != Learner {
				rf.mu.Unlock()
				return
			}
			if rf.hasPendingAddVoterOrRemove() {
				rf.mu.Unlock()
				continue
			}
			addr := rf.memberAddrs[target]
			lastIdx, _ := rf.getLastLogInfo()
			match := rf.matchIndex[target]
			delta := int(lastIdx - match)
			th := rf.cfg.CatchUpPromoteThreshold
			if delta >= th {
				rf.mu.Unlock()
				continue
			}
			cmd, err := encodeConfigAdd(target, addr)
			if err != nil {
				rf.mu.Unlock()
				rf.logf(slog.LevelWarn, "catch-up promote encode for node %d: %v", target, err)
				return
			}
			entry := LogEntry{
				Type:    EntryAddVoter,
				Term:    rf.currentTerm,
				Command: cmd,
			}
			rf.log = append(rf.log, entry)
			index := LogIndex(len(rf.log) - 1)
			rf.rebuildVolatilePeers(index)
			rf.logf(slog.LevelInfo, "Node %d caught up (delta=%d < %d), promoting to VOTER at index %d", target, delta, th, index)
			rf.broadcastAppendEntries()
			rf.mu.Unlock()
			return
		}
	}
}
