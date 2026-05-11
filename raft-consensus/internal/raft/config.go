package raft

import "time"

type Config struct {
	HeartbeatInterval           time.Duration
	MinElectionTimeout          time.Duration
	MaxElectionTimeout          time.Duration
	RPCTimeout                  time.Duration
	LeaderQuorumLivenessTimeout time.Duration
	CatchUpPromoteThreshold     int
}

func DefaultTestingConfig() Config {
	return Config{
		HeartbeatInterval:           5 * time.Second,
		MinElectionTimeout:          10 * time.Second,
		MaxElectionTimeout:          15 * time.Second,
		RPCTimeout:                  1 * time.Second,
		LeaderQuorumLivenessTimeout: 15 * time.Second,
		CatchUpPromoteThreshold:     10,
	}
}

func DefaultProductionConfig() Config {
	return Config{
		HeartbeatInterval:           50 * time.Millisecond,
		MinElectionTimeout:          150 * time.Millisecond,
		MaxElectionTimeout:          300 * time.Millisecond,
		RPCTimeout:                  5 * time.Second,
		LeaderQuorumLivenessTimeout: 300 * time.Millisecond,
		CatchUpPromoteThreshold:     10,
	}
}

// normalize fills zero or negative fields from DefaultConfig().
func (c Config) normalize() Config {
	d := DefaultProductionConfig()
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = d.HeartbeatInterval
	}
	if c.MinElectionTimeout <= 0 {
		c.MinElectionTimeout = d.MinElectionTimeout
	}
	if c.MaxElectionTimeout <= 0 {
		c.MaxElectionTimeout = d.MaxElectionTimeout
	}
	if c.RPCTimeout <= 0 {
		c.RPCTimeout = d.RPCTimeout
	}
	if c.LeaderQuorumLivenessTimeout <= 0 {
		c.LeaderQuorumLivenessTimeout = d.LeaderQuorumLivenessTimeout
	}
	if c.CatchUpPromoteThreshold <= 0 {
		c.CatchUpPromoteThreshold = d.CatchUpPromoteThreshold
	}
	return c
}
