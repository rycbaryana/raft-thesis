package raft

import (
	"io"
)

// StateMachine is an application-provided replicated state machine.
// Raft owns apply ordering and invokes Apply for committed commands.
type StateMachine interface {
	Apply(command []byte) (any, error)
}

// SnapshotStateMachine extends the FSM contract with snapshot hooks.
type SnapshotStateMachine interface {
	StateMachine
	Snapshot(w io.Writer) error
	Restore(r io.Reader) error
}
