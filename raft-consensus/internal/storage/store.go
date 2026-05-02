package storage

import "errors"

var ErrKeyNotFound = errors.New("key not found")

type Store interface {
	Put(key, value string) error
	Get(key string) (string, error)
}
