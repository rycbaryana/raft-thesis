package raft

import "time"

// const (
// 	heartbeatTime      time.Duration = 50 * time.Millisecond
// 	minElectionTimeout time.Duration = 150 * time.Millisecond
// 	maxElectionTimeout time.Duration = 300 * time.Millisecond
// )

const (
	heartbeatTime      time.Duration = 5 * time.Second
	minElectionTimeout time.Duration = 10 * time.Second
	maxElectionTimeout time.Duration = 15 * time.Second
)
