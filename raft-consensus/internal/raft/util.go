package raft

import (
	"math/rand/v2"
	"time"
)

func (rf *Raft) getLastLogInfo() (LogIndex, Term) {
	if len(rf.log) > 0 {
		idx := len(rf.log) - 1
		return LogIndex(idx), rf.log[idx].Term
	}
	return 0, 0
}

func getRandomDuration(min time.Duration, max time.Duration) time.Duration {
	return time.Duration(rand.Int64N(int64(max - min)) + int64(min))
}
