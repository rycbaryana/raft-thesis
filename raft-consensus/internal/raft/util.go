package raft

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"
)

func (rf *Raft) logf(level slog.Level, format string, args ...any) {
	if rf.logger == nil {
		return
	}
	stateStr := strings.ToUpper(rf.state.String())
	msg := fmt.Sprintf(format, args...)

	rf.logger.LogAttrs(context.Background(), level, msg,
		slog.Int("node", int(rf.id)),
		slog.Int("term", int(rf.currentTerm)),
		slog.String("state", stateStr),
	)
}

func (rf *Raft) getLastLogInfo() (LogIndex, Term) {
	if len(rf.log) > 0 {
		idx := len(rf.log) - 1
		return LogIndex(idx), rf.log[idx].Term
	}
	return 0, 0
}

// quorumVotes returns the votes needed for a majority of voting members only (learners excluded).
// Caller must hold rf.mu.
func (rf *Raft) quorumVotes() int {
	n := len(rf.voterMembers)
	if n == 0 {
		return 1
	}
	return n/2 + 1
}

func getRandomDuration(min time.Duration, max time.Duration) time.Duration {
	return time.Duration(rand.Int64N(int64(max-min)) + int64(min))
}
