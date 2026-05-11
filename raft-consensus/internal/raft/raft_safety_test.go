package raft

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRaft_CrashRecovery(t *testing.T) {
	tc := NewTestCluster(t, 3)
	leader := tc.WaitForLeader()
	follower := tc.AnyFollower(leader.ID)

	t.Logf("[SCENARIO] CrashRecovery checks follower catch-up and FSM convergence")
	t.Logf("[STEP] elected leader=%s isolatedFollower=%s", leader.ID, follower.ID)
	logClusterState(t, tc, "before_isolation")

	t.Logf("[STEP] isolate follower=%s (drop incoming/outgoing RPC)", follower.ID)
	tc.Isolate(follower.ID)
	logClusterState(t, tc, "after_isolation")

	for i := 1; i <= 2; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := fmt.Sprintf("val-%d", i)
		t.Logf("[STEP] append write #%d key=%s value=%s via leader=%s", i, key, value, leader.ID)
		require.Eventually(t, func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancel()
			err := leader.Put(ctx, key, value)
			if err == nil {
				return true
			}
			if errors.Is(err, ErrNotLeader) {
				if current := tc.CurrentLeader(); current != nil {
					leader = current
				}
			}
			return false
		}, 5*time.Second, 25*time.Millisecond, "failed to put %s", key)

		logLeaderProgress(t, tc, leader.ID, i)
	}

	t.Logf("[STEP] reconnect follower=%s and wait until commit index catches up", follower.ID)
	tc.Reconnect(follower.ID)
	logClusterState(t, tc, "after_reconnect")

	restored := tc.Node(follower.ID)
	require.Eventually(t, func() bool {
		currentLeader := tc.CurrentLeader()
		if currentLeader == nil {
			return false
		}
		return restored.CommitIndex() >= currentLeader.CommitIndex()
	}, 5*time.Second, 25*time.Millisecond, "restored node did not catch up commit index")

	currentLeader := tc.WaitForLeader()
	t.Logf("[ASSERT] follower_caught_up follower=%s followerCommit=%d leader=%s leaderCommit=%d",
		restored.ID, restored.CommitIndex(), currentLeader.ID, currentLeader.CommitIndex())

	var baseline map[string]string
	for i, nodeID := range tc.order {
		snapshot := tc.Node(nodeID).Snapshot()
		if i == 0 {
			baseline = snapshot
			continue
		}
		require.Equal(t, baseline, snapshot, "state machine mismatch on %s", nodeID)
	}
	t.Logf("[ASSERT] state_machine_equal snapshot=%s", formatSnapshot(baseline))
	logClusterState(t, tc, "final")
}

func TestRaft_SplitBrain_And_ReadIndex(t *testing.T) {
	tc := NewTestCluster(t, 3)
	leader1 := tc.WaitForLeader()
	t.Logf("[SCENARIO] SplitBrain_ReadIndex checks old isolated leader cannot serve linearizable read")
	t.Logf("[STEP] initial leader=%s", leader1.ID)

	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		return leader1.Put(ctx, "key", "val1") == nil
	}, 5*time.Second, 25*time.Millisecond, "initial put failed")
	t.Logf("[STEP] baseline_write key=key value=val1 leader=%s", leader1.ID)
	logClusterState(t, tc, "before_split")

	t.Logf("[STEP] isolate old leader=%s to create split", leader1.ID)
	tc.Isolate(leader1.ID)
	logClusterState(t, tc, "after_leader_isolation")

	leader2 := tc.WaitForNewLeader(leader1.ID)
	t.Logf("[STEP] new leader elected among majority=%s", leader2.ID)

	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		err := leader2.Put(ctx, "key", "val2")
		if err == nil {
			return true
		}
		if errors.Is(err, ErrNotLeader) {
			if current := tc.CurrentLeader(); current != nil && current.ID != leader1.ID {
				leader2 = current
			}
		}
		return false
	}, 5*time.Second, 25*time.Millisecond, "put on new leader failed")
	t.Logf("[STEP] majority_write key=key value=val2 leader=%s", leader2.ID)
	logClusterState(t, tc, "after_majority_write")

	readCtx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	value, err := leader1.Get(readCtx, "key")
	t.Logf("[ASSERT] isolated_read oldLeader=%s value=%q err=%v", leader1.ID, value, err)

	require.Error(t, err, "isolated old leader must not serve linearizable reads")
	require.NotEqual(t, "val1", value, "isolated old leader returned stale value")
	t.Logf("[ASSERT] read_index_guard_ok expected=error_or_notleader gotErr=%v", err != nil)
}

func TestRaft_CounterStress(t *testing.T) {
	tc := NewTestCluster(t, 3)
	leader := tc.WaitForLeader()
	t.Logf("[SCENARIO] CounterStress checks concurrent increments converge to exact count")
	t.Logf("[STEP] initial leader=%s", leader.ID)

	const workers = 50
	var wg sync.WaitGroup
	var retriesMu sync.Mutex
	var successMu sync.Mutex
	retries := 0
	successes := 0
	errCh := make(chan error, workers)
	t.Logf("[STEP] start stress workers=%d operation=incr(counter)", workers)

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()

			for {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("worker %d: %w", workerID, ctx.Err())
					return
				default:
				}

				current := tc.CurrentLeader()
				if current == nil {
					continue
				}

				opCtx, opCancel := context.WithTimeout(ctx, 400*time.Millisecond)
				_, err := current.Incr(opCtx, "counter")
				opCancel()
				if err == nil {
					successMu.Lock()
					successes++
					successMu.Unlock()
					return
				}

				retriesMu.Lock()
				retries++
				retriesMu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	successMu.Lock()
	t.Logf("[STEP] all workers finished successfulOps=%d", successes)
	successMu.Unlock()

	require.Eventually(t, func() bool {
		current := tc.CurrentLeader()
		if current == nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		value, err := current.Get(ctx, "counter")
		if err != nil {
			return false
		}
		got, err := strconv.Atoi(value)
		if err != nil {
			return false
		}
		return got == workers
	}, 5*time.Second, 25*time.Millisecond, "counter did not converge to %d", workers)

	finalLeader := tc.WaitForLeader()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, err := finalLeader.Get(ctx, "counter")
	require.NoError(t, err)
	got, err := strconv.Atoi(value)
	require.NoError(t, err)
	require.Equal(t, workers, got)

	retriesMu.Lock()
	retryCount := retries
	retriesMu.Unlock()
	t.Logf("[ASSERT] counter_exact workers=%d retries=%d expected=%d actual=%d", workers, retryCount, workers, got)
	t.Logf("[STATE] final_snapshot=%s", formatSnapshot(finalLeader.Snapshot()))
	logClusterState(t, tc, "final")
}

func logLeaderProgress(t *testing.T, tc *TestCluster, leaderID string, writeNo int) {
	t.Helper()
	leader := tc.Node(leaderID)
	t.Logf("[STATE] write=%d leader=%s leaderCommit=%d", writeNo, leader.ID, leader.CommitIndex())
}

func logClusterState(t *testing.T, tc *TestCluster, stage string) {
	t.Helper()
	for _, nodeID := range tc.order {
		node := tc.Node(nodeID)
		node.raft.mu.Lock()
		state := node.raft.state
		term := node.raft.currentTerm
		commit := node.raft.commitIndex
		applied := node.raft.lastApplied
		node.raft.mu.Unlock()
		t.Logf("[STATE] stage=%s node=%s role=%s term=%d commit=%d applied=%d snapshot=%s",
			stage, node.ID, state.String(), term, commit, applied, formatSnapshot(node.Snapshot()))
	}
}

func formatSnapshot(snapshot map[string]string) string {
	if len(snapshot) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(snapshot))
	for k := range snapshot {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, snapshot[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
