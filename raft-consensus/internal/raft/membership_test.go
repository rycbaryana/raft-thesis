package raft

import (
	"errors"
	"testing"
	"time"
)

func TestRemoveNodeNotInCluster(t *testing.T) {
	rf := newTestRaft()
	rf.state = Leader
	rf.currentTerm = 1

	err := rf.RemoveNode(99)
	if !errors.Is(err, ErrNodeNotInCluster) {
		t.Fatalf("expected ErrNodeNotInCluster, got %v", err)
	}
}

func TestConfigChangeRuleOfOneBlocksAddNodeWhenPromotePending(t *testing.T) {
	rf := newTestRaft()
	rf.state = Leader
	rf.currentTerm = 1
	cmd, err := encodeConfigAdd(4, "localhost:9999")
	if err != nil {
		t.Fatal(err)
	}
	rf.mu.Lock()
	rf.log = append(rf.log, LogEntry{Type: EntryAddVoter, Term: 1, Command: cmd})
	rf.commitIndex = 0
	rf.rebuildVolatilePeers(LogIndex(len(rf.log) - 1))
	rf.mu.Unlock()

	if !rf.hasPendingAddVoterOrRemove() {
		t.Fatal("expected pending add voter or remove")
	}

	err = rf.AddNode(5, "localhost:9998")
	if !errors.Is(err, ErrConfigChangeInProgress) {
		t.Fatalf("expected ErrConfigChangeInProgress, got %v", err)
	}
}

func TestTruncateRestoresMembershipFromLog(t *testing.T) {
	rf := newTestRaft()
	rf.currentTerm = 1

	add4, err := encodeConfigAdd(4, "h4")
	if err != nil {
		t.Fatal(err)
	}
	rf.mu.Lock()
	rf.log = append(rf.log, LogEntry{Type: EntryAddLearner, Term: 1, Command: add4})
	rf.rebuildVolatilePeers(LogIndex(len(rf.log) - 1))
	if _, ok := rf.activeMembers[4]; !ok {
		t.Fatal("expected node 4 in activeMembers after add learner entry")
	}
	if rf.isVoter(4) {
		t.Fatal("expected node 4 to be learner, not voter")
	}
	rf.mu.Unlock()

	add5, err := encodeConfigAdd(5, "h5")
	if err != nil {
		t.Fatal(err)
	}
	args := &AppendEntriesArgs{
		Term:         2,
		LeaderID:     2,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries: []LogEntry{
			{Type: EntryAddLearner, Term: 2, Command: add5},
		},
	}
	reply := &AppendEntriesReply{}
	if err := rf.AppendEntries(args, reply); err != nil {
		t.Fatal(err)
	}
	if !reply.Success {
		t.Fatalf("expected success, got %+v", reply)
	}

	rf.mu.Lock()
	_, has4 := rf.activeMembers[4]
	_, has5 := rf.activeMembers[5]
	rf.mu.Unlock()
	if has4 {
		t.Fatal("expected node 4 removed after log truncate")
	}
	if !has5 {
		t.Fatal("expected node 5 present from new leader entry")
	}
}

func TestCommitQuorumIgnoresLeaderNotInConfig(t *testing.T) {
	rf := newTestRaft()
	rf.state = Leader
	rf.currentTerm = 5
	rf.commitIndex = 0
	rf.id = 1
	rf.activeMembers = map[NodeID]struct{}{2: {}, 3: {}}
	rf.voterMembers = map[NodeID]struct{}{2: {}, 3: {}}
	rf.memberAddrs = map[NodeID]string{2: "", 3: ""}
	rf.log = []LogEntry{
		{Term: 0},
		{Term: 5},
		{Term: 5},
	}
	rf.matchIndex[2] = 0
	rf.matchIndex[3] = 0

	rf.updateCommitIndex()
	if rf.commitIndex != 0 {
		t.Fatalf("expected no commit without replicas, got %d", rf.commitIndex)
	}

	rf.matchIndex[2] = 2
	rf.updateCommitIndex()
	if rf.commitIndex != 0 {
		t.Fatalf("expected no commit with only one replica ack, got %d", rf.commitIndex)
	}

	rf.matchIndex[3] = 2
	rf.updateCommitIndex()
	if rf.commitIndex != 2 {
		t.Fatalf("expected commit index 2 with quorum of two followers, got %d", rf.commitIndex)
	}
}

func TestApplierSkipsFSMForConfigEntries(t *testing.T) {
	rf := newTestRaft()
	fsm := &recordingFSM{}
	rf.fsm = fsm
	addCmd, err := encodeConfigAdd(2, "x")
	if err != nil {
		t.Fatal(err)
	}
	rf.log = []LogEntry{
		{Term: 0},
		{Type: EntryAddLearner, Term: 1, Command: addCmd},
	}
	rf.commitIndex = 1

	go rf.applier()

	deadline := time.After(500 * time.Millisecond)
	for {
		fsm.mu.Lock()
		n := len(fsm.seen)
		fsm.mu.Unlock()
		if n > 0 {
			t.Fatalf("FSM should not apply config entries, saw %#v", fsm.seen)
		}
		rf.mu.Lock()
		done := rf.lastApplied >= 1
		rf.mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for lastApplied")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestLearnerRejectsRequestVote(t *testing.T) {
	rf := newTestRaft()
	rf.currentTerm = 1
	rf.mu.Lock()
	rf.voterMembers = map[NodeID]struct{}{}
	rf.activeMembers = map[NodeID]struct{}{rf.id: {}}
	rf.mu.Unlock()

	args := &RequestVoteArgs{
		Term:         2,
		CandidateID:  2,
		LastLogIndex: 0,
		LastLogTerm:  0,
	}
	reply := &RequestVoteReply{}
	if err := rf.RequestVote(args, reply); err != nil {
		t.Fatal(err)
	}
	if reply.VoteGranted {
		t.Fatal("learner (non-voter) must not grant vote")
	}
}
