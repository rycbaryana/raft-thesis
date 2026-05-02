package raft

import "log/slog"

type RaftService interface {
	// RequestVote is invoked by candidates to gather votes.
	RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error

	// AppendEntries is invoked by the leader to replicate log entries
	// and serves as a heartbeat.
	AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.logf(slog.LevelDebug, "Got AppendEntries from %d (term %d) with %d entries, prevLogIndex: %d, prevLogTerm: %d, leaderCommit: %d", args.LeaderID, args.Term, len(args.Entries), args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommit)
	// 1. Term check
	if args.Term < rf.currentTerm {
		reply.Success = false
		reply.Term = rf.currentTerm
		rf.logf(slog.LevelDebug, "AppendEntries reply to %d: success=false term=%d (stale leader term)", args.LeaderID, reply.Term)
		return nil
	}

	// 2. Back to Follower if valid leader contacts us
	rf.resetElectionTimer()
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
	}
	rf.hintLeaderID = args.LeaderID
	reply.Term = rf.currentTerm

	// 3. Log Consistency Check
	// Does our log have entry at PrevLogIndex with PrevLogTerm?
	lastLogIndex, _ := rf.getLastLogInfo()

	if args.PrevLogIndex > lastLogIndex {
		reply.Success = false
		reply.ConflictIndex = lastLogIndex + 1
		reply.ConflictTerm = 0
		rf.logf(slog.LevelDebug, "AppendEntries reply to %d: success=false term=%d conflictIndex=%d conflictTerm=%d (prev log missing)",
			args.LeaderID, reply.Term, reply.ConflictIndex, reply.ConflictTerm)
		return nil
	}

	if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.Success = false
		reply.ConflictTerm = rf.log[args.PrevLogIndex].Term

		conflictIndex := args.PrevLogIndex
		for conflictIndex > 1 && rf.log[conflictIndex-1].Term == reply.ConflictTerm {
			conflictIndex--
		}
		reply.ConflictIndex = conflictIndex

		rf.logf(slog.LevelDebug, "AppendEntries reply to %d: success=false term=%d conflictIndex=%d conflictTerm=%d (prev log term mismatch)",
			args.LeaderID, reply.Term, reply.ConflictIndex, reply.ConflictTerm)
		return nil
	}

	// 4. Append New Entries
	for i, entry := range args.Entries {
		index := args.PrevLogIndex + 1 + LogIndex(i)
		logSize := LogIndex(len(rf.log))

		if index < logSize {
			if rf.log[index].Term != entry.Term {
				rf.log = append(rf.log[:index], entry)
			}
		} else {
			rf.log = append(rf.log, entry)
		}
	}

	// 5. Update Commit Index
	if args.LeaderCommit > rf.commitIndex {
		lastNewIndex := args.PrevLogIndex + LogIndex(len(args.Entries))
		rf.commitIndex = min(args.LeaderCommit, lastNewIndex)

		rf.applyCond.Broadcast()
	}

	reply.Success = true
	if len(args.Entries) > 0 {
		rf.logf(slog.LevelDebug, "AppendEntries reply to %d: success=true term=%d entries=%d leaderCommit=%d ourCommit=%d",
			args.LeaderID, reply.Term, len(args.Entries), args.LeaderCommit, rf.commitIndex)
	}
	return nil
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.logf(slog.LevelDebug, "Got RequestVote from %d (term %d)", args.CandidateID, args.Term)
	// 1. Term check
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		return nil
	}

	// 2. Step down if fresher term
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
	}
	reply.Term = rf.currentTerm

	// 3. Check if already voted
	if rf.votedFor != NoNode && rf.votedFor != args.CandidateID {
		reply.VoteGranted = false
		return nil
	}

	// 4. Log should be up-to-date
	lastLogIndex, lastLogTerm := rf.getLastLogInfo()

	isLogUpToDate := false
	if args.LastLogTerm > lastLogTerm {
		isLogUpToDate = true
	} else if args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex {
		isLogUpToDate = true
	}

	if isLogUpToDate {
		rf.votedFor = args.CandidateID
		rf.resetElectionTimer()
		reply.VoteGranted = true
		rf.logf(slog.LevelInfo, "Voted FOR %d in term %d", args.CandidateID, rf.currentTerm)
	} else {
		reply.VoteGranted = false
	}

	return nil
}
