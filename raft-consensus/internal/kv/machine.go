package kv

import (
	"errors"
	"fmt"

	"raft-consensus/internal/storage"
)

var ErrKeyNotFound = errors.New("kv: key not found")

type Machine struct {
	store storage.Store
}

func NewMachine(store storage.Store) *Machine {
	return &Machine{store: store}
}

func (m *Machine) Apply(command []byte) (any, error) {
	cmd, err := DecodeCommand(command)
	if err != nil {
		return nil, fmt.Errorf("decode command: %w", err)
	}
	if err := validateCommand(cmd); err != nil {
		return nil, err
	}
	return nil, m.store.Put(cmd.Key, cmd.Value)
}

func (m *Machine) Get(key string) (string, error) {
	value, err := m.store.Get(key)
	if err != nil {
		if errors.Is(err, storage.ErrKeyNotFound) {
			return "", ErrKeyNotFound
		}
		return "", err
	}
	return value, nil
}
