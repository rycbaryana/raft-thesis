package kv

import (
	"encoding/json"
	"fmt"
)

type OpType string

const (
	OpPut OpType = "PUT"
)

type Command struct {
	Type  OpType `json:"type"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

func EncodePut(key, value string) ([]byte, error) {
	return json.Marshal(Command{
		Type:  OpPut,
		Key:   key,
		Value: value,
	})
}

func DecodeCommand(raw []byte) (Command, error) {
	var cmd Command
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return Command{}, err
	}
	return cmd, nil
}

func validateCommand(cmd Command) error {
	if cmd.Type != OpPut {
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
	if cmd.Key == "" {
		return fmt.Errorf("empty key")
	}
	return nil
}
