package storage

import (
	"errors"
	"testing"
)

func TestMemoryStorePutGet(t *testing.T) {
	s := NewMemoryStore()
	if err := s.Put("k", "v"); err != nil {
		t.Fatalf("put failed: %v", err)
	}
	got, err := s.Get("k")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got != "v" {
		t.Fatalf("unexpected value: %q", got)
	}
}

func TestMemoryStoreMissingKey(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.Get("missing")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}
