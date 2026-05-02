package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

type diskRecord struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type DiskStore struct {
	mu   sync.RWMutex
	path string
	data map[string]string
	file *os.File
}

func NewDiskStore(path string) (*DiskStore, error) {
	if path == "" {
		return nil, fmt.Errorf("disk store path is empty")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open disk store: %w", err)
	}
	store := &DiskStore{
		path: path,
		data: make(map[string]string),
		file: file,
	}
	if err := store.restore(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return store, nil
}

func (s *DiskStore) restore() error {
	if _, err := s.file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek disk store: %w", err)
	}
	scanner := bufio.NewScanner(s.file)
	for scanner.Scan() {
		var rec diskRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return fmt.Errorf("decode disk record: %w", err)
		}
		s.data[rec.Key] = rec.Value
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan disk store: %w", err)
	}
	_, err := s.file.Seek(0, io.SeekEnd)
	return err
}

func (s *DiskStore) Put(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := diskRecord{Key: key, Value: value}
	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode disk record: %w", err)
	}
	if _, err := s.file.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write disk record: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync disk record: %w", err)
	}
	s.data[key] = value
	return nil
}

func (s *DiskStore) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.data[key]
	if !ok {
		return "", ErrKeyNotFound
	}
	return value, nil
}

func (s *DiskStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}
