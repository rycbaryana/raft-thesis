package raft

import (
	"errors"
	"io"
)

var (
	ErrConfigChangeUnimplemented = errors.New("raft: config change not implemented")
)

type SnapshotSink interface {
	io.Writer
	Close() error
	Cancel() error
}

type SnapshotStore interface {
	Create(lastIncludedIndex LogIndex, lastIncludedTerm Term) (SnapshotSink, error)
	Open(lastIncludedIndex LogIndex) (io.ReadCloser, error)
}

type ConfigChangeType int

const (
	ConfigChangeAddNode ConfigChangeType = iota + 1
	ConfigChangeRemoveNode
)

type ConfigChange struct {
	Type    ConfigChangeType
	NodeID  NodeID
	Address string
}

type ClusterConfig struct {
	Version uint64
	Members []NodeID
}

func (rf *Raft) ProposeConfigChange(_ ConfigChange) error {
	return ErrConfigChangeUnimplemented
}
