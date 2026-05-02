package raft

import "errors"

var (
	ErrNotLeader = errors.New("raft: not leader")
	// ErrReadIndexNoQuorum is returned when a quorum did not acknowledge the read-index heartbeat.
	ErrReadIndexNoQuorum = errors.New("raft: read index quorum not reached")
)
