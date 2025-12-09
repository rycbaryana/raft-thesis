package raft

type RaftService interface {
	// RequestVote is invoked by candidates to gather votes.
	RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error

	// AppendEntries is invoked by the leader to replicate log entries
	// and serves as a heartbeat.
	AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error
}