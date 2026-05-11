package raft

import (
	"encoding/json"
	"fmt"
)

type configAddPayload struct {
	ID   NodeID `json:"id"`
	Addr string `json:"addr"`
}

type configRemovePayload struct {
	ID NodeID `json:"id"`
}

func encodeConfigAdd(id NodeID, addr string) ([]byte, error) {
	b, err := json.Marshal(configAddPayload{ID: id, Addr: addr})
	if err != nil {
		return nil, fmt.Errorf("raft: encode config add: %w", err)
	}
	return b, nil
}

func encodeConfigRemove(id NodeID) ([]byte, error) {
	b, err := json.Marshal(configRemovePayload{ID: id})
	if err != nil {
		return nil, fmt.Errorf("raft: encode config remove: %w", err)
	}
	return b, nil
}

func decodeConfigAdd(cmd []byte) (id NodeID, addr string, err error) {
	var p configAddPayload
	if err := json.Unmarshal(cmd, &p); err != nil {
		return 0, "", fmt.Errorf("raft: decode config add: %w", err)
	}
	return p.ID, p.Addr, nil
}

func decodeConfigRemove(cmd []byte) (id NodeID, err error) {
	var p configRemovePayload
	if err := json.Unmarshal(cmd, &p); err != nil {
		return 0, fmt.Errorf("raft: decode config remove: %w", err)
	}
	return p.ID, nil
}
