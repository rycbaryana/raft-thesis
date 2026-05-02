package raft

import (
	"context"
	"log/slog"
	"time"
)

// ReadIndex confirms leadership with a quorum via AppendEntries heartbeats and returns
// the commit index that linearizable reads must wait to be applied locally.
func (rf *Raft) ReadIndex(ctx context.Context) (LogIndex, error) {
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

	votesCh := make(chan bool, len(rf.peers))

	rf.logf(slog.LevelDebug, "ReadIndex: collecting votes from peers")
	for peerID, peer := range rf.peers {
		if peerID == rf.id {
			continue
		}

		go func(id NodeID, p RaftService) {
			success := rf.sendHeartbeat(id, p)
			votesCh <- success
		}(peerID, peer)
	}

	acks := 1
	denials := 0
	quorum := rf.quorumVotes()
	totalPeers := len(rf.peers)

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

				if totalPeers-denials < quorum {
					rf.logf(slog.LevelDebug, "ReadIndex: no quorum of %d votes", quorum)
					return 0, ErrReadIndexNoQuorum
				}
			}
		}
	}
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
