package raft

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type selectiveErrFSM struct {
	mu      sync.Mutex
	failOn  map[string]error
	applied []string
}

func (f *selectiveErrFSM) Apply(command []byte) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := string(command)
	f.applied = append(f.applied, key)
	if err, ok := f.failOn[key]; ok {
		return nil, err
	}
	return nil, nil
}

func TestWaitCommittedFutureCancellation(t *testing.T) {
	rf := newTestRaft()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := rf.waitForApplied(ctx, 5)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if len(rf.applyFutures) != 0 {
		t.Fatalf("expected no leaked futures, got %d", len(rf.applyFutures))
	}
}

func TestWaitCommittedMultipleSubscribersSameIndex(t *testing.T) {
	rf := newTestRaft()
	fsm := &selectiveErrFSM{}
	rf.fsm = fsm
	rf.log = []LogEntry{
		{Term: 0},
		{Term: 1, Command: []byte("a")},
	}
	go rf.applier()

	errCh := make(chan error, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() { errCh <- rf.waitForApplied(ctx, 1) }()
	go func() { errCh <- rf.waitForApplied(ctx, 1) }()

	time.Sleep(20 * time.Millisecond)
	rf.mu.Lock()
	rf.commitIndex = 1
	rf.applyCond.Broadcast()
	rf.mu.Unlock()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("subscriber %d got error: %v", i, err)
		}
	}
}

func TestWaitCommittedPropagatesApplyError(t *testing.T) {
	applyErr := errors.New("fsm apply failed")
	rf := newTestRaft()
	rf.fsm = &selectiveErrFSM{failOn: map[string]error{"bad": applyErr}}
	rf.log = []LogEntry{
		{Term: 0},
		{Term: 1, Command: []byte("bad")},
	}
	go rf.applier()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- rf.waitForApplied(ctx, 1)
	}()

	time.Sleep(20 * time.Millisecond)
	rf.mu.Lock()
	rf.commitIndex = 1
	rf.applyCond.Broadcast()
	rf.mu.Unlock()

	err := <-errCh
	if !errors.Is(err, applyErr) {
		t.Fatalf("expected apply error propagation, got %v", err)
	}
}
