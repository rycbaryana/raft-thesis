package raft

import (
	"context"
	"log/slog"
	"time"
)

// ReadIndex confirms leadership with a quorum via AppendEntries heartbeats and returns
// the commit index that linearizable reads must wait to be applied locally.
func (rf *Raft) ReadIndex(ctx context.Context) (LogIndex, error) {
	// Fast-path: linearizable lease read when quorum freshness is known.
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return 0, ErrNotLeader
	}
	if rf.log[rf.commitIndex].Term == rf.currentTerm && rf.hasLeaderLeaseLocked(time.Now()) {
		target := rf.commitIndex
		rf.mu.Unlock()
		return target, nil
	}
	rf.mu.Unlock()

	rf.readIndexMu.Lock()
	defer rf.readIndexMu.Unlock()

	var leaderTerm Term
	var targetIndex LogIndex

	// Ожидание фиксации No-Op
	for {
		rf.mu.Lock()
		if rf.state != Leader {
			rf.mu.Unlock()
			return 0, ErrNotLeader
		}

		if rf.log[rf.commitIndex].Term == rf.currentTerm {
			leaderTerm = rf.currentTerm
			targetIndex = rf.commitIndex
			// if rf.hasLeaderLeaseLocked(time.Now()) {
			// 	rf.logf(slog.LevelDebug, "ReadIndex: leader lease is fresh, serving read without quorum round")
			// 	rf.mu.Unlock()
			// 	return targetIndex, nil
			// }
			rf.mu.Unlock()
			break
		}
		rf.logf(slog.LevelDebug, "ReadIndex: waiting for leader to commit a no-op")
		rf.mu.Unlock()

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(rf.cfg.HeartbeatInterval):
		}
	}

	rf.mu.Lock()
	quorum := rf.quorumVotes()
	type hbTarget struct {
		id NodeID
		c  RaftService
	}
	var targets []hbTarget
	for peerID, peer := range rf.peers {
		if peerID == rf.id || peer == nil || peer.Client == nil {
			continue
		}
		if !rf.isVoter(peerID) {
			continue
		}
		targets = append(targets, hbTarget{id: peerID, c: peer.Client})
	}
	selfInCluster := rf.isVoter(rf.id)
	rf.mu.Unlock()

	votesCh := make(chan bool, len(targets))
	rf.logf(slog.LevelDebug, "ReadIndex: collecting votes from peers")
	for _, t := range targets {
		go func(id NodeID, p RaftService) {
			success := rf.sendAppendEntries(id)
			votesCh <- success
		}(t.id, t.c)
	}

	acks := 0
	if selfInCluster {
		acks = 1
	}
	denials := 0
	maxPossibleAcks := len(targets)
	if selfInCluster {
		maxPossibleAcks++
	}

	if acks >= quorum {
		return targetIndex, nil
	}

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()

		case success := <-votesCh:
			if success {
				acks++

				if acks >= quorum {
					rf.mu.Lock()
					defer rf.mu.Unlock()
					if rf.state != Leader || rf.currentTerm != leaderTerm {
						rf.logf(slog.LevelDebug, "ReadIndex: leader changed, returning error")
						return 0, ErrNotLeader
					}
					rf.logf(slog.LevelDebug, "ReadIndex: quorum of %d votes collected", quorum)
					return targetIndex, nil
				}
			} else {
				denials++

				if maxPossibleAcks-denials < quorum {
					rf.logf(slog.LevelDebug, "ReadIndex: no quorum of %d votes", quorum)
					return 0, ErrReadIndexNoQuorum
				}
			}
		}
	}
}

func (rf *Raft) hasLeaderLeaseLocked(now time.Time) bool {
	live := 0
	if rf.isVoter(rf.id) {
		live = 1
	}
	for peerID := range rf.voterMembers {
		if peerID == rf.id {
			continue
		}
		if ackAt, ok := rf.lastHeartbeatAck[peerID]; ok && now.Sub(ackAt) < rf.cfg.MinElectionTimeout {
			live++
		}
	}
	return live >= rf.quorumVotes()
}

func (rf *Raft) ReadBarrier(ctx context.Context) error {
	index, err := rf.ReadIndex(ctx)
	if err != nil {
		return err
	}
	rf.logf(slog.LevelDebug, "ReadBarrier: waiting for index %d to be applied", index)
	if err := rf.waitForApplied(ctx, index); err != nil {
		return err
	}
	return nil
}
