package network

import (
	"sync/atomic"

	"raft-consensus/internal/raft"
)

type RaftRPCServerAdapter struct {
	inner   *raft.Raft
	enabled atomic.Bool
}

func NewRaftRPCServerAdapter(rf *raft.Raft) *RaftRPCServerAdapter {
	a := &RaftRPCServerAdapter{inner: rf}
	a.enabled.Store(true)
	return a
}

func (a *RaftRPCServerAdapter) DisconnectIncoming() {
	a.enabled.Store(false)
}

func (a *RaftRPCServerAdapter) ReconnectIncoming() {
	a.enabled.Store(true)
}

func (a *RaftRPCServerAdapter) RequestVote(args *raft.RequestVoteArgs, reply *raft.RequestVoteReply) error {
	if !a.enabled.Load() {
		return ErrNetworkPartition
	}
	return a.inner.RequestVote(args, reply)
}

func (a *RaftRPCServerAdapter) AppendEntries(args *raft.AppendEntriesArgs, reply *raft.AppendEntriesReply) error {
	if !a.enabled.Load() {
		return ErrNetworkPartition
	}
	return a.inner.AppendEntries(args, reply)
}
