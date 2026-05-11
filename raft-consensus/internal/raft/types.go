package raft

type Term uint64
type LogIndex uint64
type NodeID int

const (
	NoNode NodeID = -1
)

type State int

const (
	Follower State = iota
	Candidate
	Leader
)

func (s State) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

type EntryType int

const (
	EntryNormal EntryType = iota
	EntryAddLearner
	EntryAddVoter
	EntryRemoveNode
)

type LogEntry struct {
	Type    EntryType
	Term    Term
	Command []byte
}

type PeerRole int

const (
	Voter PeerRole = iota
	Learner
)

type Peer struct {
	Client RaftService
	Role   PeerRole
}

type PeerFactory func(id NodeID, addr string) RaftService

type ApplyMsg struct {
	CommandValid bool
	Command      []byte
	CommandIndex LogIndex
}

type RequestVoteArgs struct {
	Term         Term
	CandidateID  NodeID
	LastLogIndex LogIndex
	LastLogTerm  Term
}

type RequestVoteReply struct {
	Term        Term
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         Term
	LeaderID     NodeID
	PrevLogIndex LogIndex
	PrevLogTerm  Term
	Entries      []LogEntry
	LeaderCommit LogIndex
}

type AppendEntriesReply struct {
	Term    Term
	Success bool

	ConflictIndex LogIndex
	ConflictTerm  Term
}
