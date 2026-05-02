package raft

import "context"

type indexFuture struct {
	done     chan struct{}
	resolved bool
	err      error
	waiters  int
}

func (rf *Raft) getFuture(index LogIndex) *indexFuture {
	f, ok := rf.applyFutures[index]
	if ok {
		return f
	}
	f = &indexFuture{done: make(chan struct{})}
	rf.applyFutures[index] = f
	return f
}

func (rf *Raft) resolveFutures(upTo LogIndex) {
	for idx, f := range rf.applyFutures {
		if idx > upTo || f.resolved {
			continue
		}
		close(f.done)
		f.resolved = true
		if f.waiters == 0 {
			delete(rf.applyFutures, idx)
		}
	}
}

func (rf *Raft) waitForApplied(ctx context.Context, index LogIndex) error {
	rf.mu.Lock()
	if rf.lastApplied >= index {
		rf.mu.Unlock()
		return nil
	}
	future := rf.getFuture(index)
	future.waiters++
	rf.mu.Unlock()

	defer func() {
		rf.mu.Lock()
		future.waiters--
		if future.waiters == 0 {
			delete(rf.applyFutures, index)
		}
		rf.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-future.done:
		return future.err
	}
}
