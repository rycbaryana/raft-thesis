package storage

import (
	"path/filepath"
	"testing"
)

func TestDiskStorePersistsData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kv.log")

	first, err := NewDiskStore(path)
	if err != nil {
		t.Fatalf("new disk store failed: %v", err)
	}
	if err := first.Put("alpha", "1"); err != nil {
		t.Fatalf("first put failed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}

	second, err := NewDiskStore(path)
	if err != nil {
		t.Fatalf("reopen disk store failed: %v", err)
	}
	defer func() { _ = second.Close() }()

	got, err := second.Get("alpha")
	if err != nil {
		t.Fatalf("get after reopen failed: %v", err)
	}
	if got != "1" {
		t.Fatalf("expected persisted value 1, got %q", got)
	}
}
