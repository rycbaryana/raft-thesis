package kv

import (
	"context"
	"fmt"
	"log/slog"

	"raft-consensus/internal/raft"
)

type Service struct {
	rf      *raft.Raft
	machine *Machine
	logger  *slog.Logger
}

func NewService(rf *raft.Raft, machine *Machine, logger *slog.Logger) *Service {
	return &Service{
		rf:      rf,
		machine: machine,
		logger:  logger.With(slog.String("component", "kv-app")),
	}
}

func (s *Service) Logger() *slog.Logger {
	return s.logger
}

func (s *Service) LeaderHint() raft.NodeID {
	return s.rf.LeaderHint()
}

func (s *Service) Put(ctx context.Context, key, value string) error {
	cmd, err := EncodePut(key, value)
	if err != nil {
		return err
	}
	isLeader, _, _, err := s.rf.SubmitAndWait(ctx, cmd)
	if !isLeader {
		return raft.ErrNotLeader
	}
	if err != nil {
		return fmt.Errorf("fsm apply error: %w", err)
	}
	return nil
}

func (s *Service) Get(ctx context.Context, key string) (string, error) {
	err := s.rf.ReadBarrier(ctx)
	if err != nil {
		return "", fmt.Errorf("read barrier: %w", err)
	}

	return s.machine.Get(key)
}

func (s *Service) AddClusterNode(ctx context.Context, id raft.NodeID, addr string) error {
	_ = ctx
	return s.rf.AddNode(id, addr)
}

func (s *Service) RemoveClusterNode(ctx context.Context, id raft.NodeID) error {
	_ = ctx
	return s.rf.RemoveNode(id)
}
