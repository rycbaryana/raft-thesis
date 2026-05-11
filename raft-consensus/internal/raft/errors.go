package raft

import "errors"

var (
	ErrNotLeader = errors.New("raft: not leader")
	// ErrReadIndexNoQuorum is returned when a quorum did not acknowledge the read-index heartbeat.
	ErrReadIndexNoQuorum = errors.New("raft: read index quorum not reached")
	// ErrConfigChangeInProgress is returned when another configuration entry is not yet committed.
	ErrConfigChangeInProgress = errors.New("raft: configuration change already in progress")
	// ErrNodeNotInCluster is returned by RemoveNode when the node is not a current member.
	ErrNodeNotInCluster = errors.New("raft: node not in cluster")
	// ErrPeerAlreadyInCluster is returned by AddNode when the id is already in the cluster.
	ErrPeerAlreadyInCluster = errors.New("raft: node already in cluster")
)
