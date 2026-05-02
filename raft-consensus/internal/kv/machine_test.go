package kv

import "testing"

import "raft-consensus/internal/storage"

func TestMachineApplyPutAndGet(t *testing.T) {
	m := NewMachine(storage.NewMemoryStore())
	cmd, err := EncodePut("k", "v")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if _, err := m.Apply(cmd); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	got, err := m.Get("k")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got != "v" {
		t.Fatalf("unexpected value: %q", got)
	}
}
