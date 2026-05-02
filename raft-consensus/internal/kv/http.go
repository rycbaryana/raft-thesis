package kv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"raft-consensus/internal/raft"
)

type NetworkPartitionToggle interface {
	SetPartitioned(partitioned bool)
}

const (
	defaultClientDeadline = 5 * time.Second
	statusClientClosed    = 499
)

type HTTPServer struct {
	service   *Service
	partition NetworkPartitionToggle
}

func NewHTTPServer(service *Service, partition NetworkPartitionToggle) *HTTPServer {
	return &HTTPServer{service: service, partition: partition}
}

func (s *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/put", s.handlePut)
	mux.HandleFunc("/get", s.handleGet)
	mux.HandleFunc("/debug/network/partition", s.handleDebugNetworkPartition)
	return mux
}

type jsonErr struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type jsonPutOK struct {
	OK bool `json:"ok"`
}

type jsonGetOK struct {
	OK    bool   `json:"ok"`
	Value string `json:"value"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *HTTPServer) requestContext(r *http.Request) (context.Context, context.CancelFunc) {
	if d, ok := r.Context().Deadline(); ok {
		remaining := time.Until(d)
		if remaining <= 0 {
			return r.Context(), func() {}
		}
		return context.WithDeadline(r.Context(), d)
	}
	return context.WithTimeout(r.Context(), defaultClientDeadline)
}

func (s *HTTPServer) setLeaderHintHeader(w http.ResponseWriter) {
	if hint := s.service.LeaderHint(); hint != raft.NoNode {
		w.Header().Set("X-Raft-Leader-Id", fmt.Sprintf("%d", hint))
	}
}

func (s *HTTPServer) handlePut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, POST, PUT")
		writeJSON(w, http.StatusMethodNotAllowed, jsonErr{OK: false, Error: "method not allowed"})
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "missing key"})
		return
	}
	val := r.URL.Query().Get("val")
	ctx, cancel := s.requestContext(r)
	defer cancel()

	err := s.service.Put(ctx, key, val)
	if err != nil {
		if errors.Is(err, raft.ErrNotLeader) {
			s.setLeaderHintHeader(w)
			writeJSON(w, http.StatusServiceUnavailable, jsonErr{OK: false, Error: "not leader"})
			return
		}
		if errors.Is(err, context.Canceled) {
			writeJSON(w, statusClientClosed, jsonErr{OK: false, Error: "request canceled"})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			writeJSON(w, http.StatusGatewayTimeout, jsonErr{OK: false, Error: "timeout waiting for commit"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, jsonErr{OK: false, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, jsonPutOK{OK: true})
}

func (s *HTTPServer) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, jsonErr{OK: false, Error: "method not allowed"})
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "missing key"})
		return
	}
	ctx, cancel := s.requestContext(r)
	defer cancel()

	value, err := s.service.Get(ctx, key)
	if err != nil {
		switch {
		case errors.Is(err, raft.ErrNotLeader):
			s.setLeaderHintHeader(w)
			writeJSON(w, http.StatusServiceUnavailable, jsonErr{OK: false, Error: "not leader"})
		case errors.Is(err, raft.ErrReadIndexNoQuorum):
			s.setLeaderHintHeader(w)
			writeJSON(w, http.StatusServiceUnavailable, jsonErr{OK: false, Error: "read index: no quorum"})
		case errors.Is(err, context.Canceled):
			writeJSON(w, statusClientClosed, jsonErr{OK: false, Error: "request canceled"})
		case errors.Is(err, context.DeadlineExceeded):
			writeJSON(w, http.StatusGatewayTimeout, jsonErr{OK: false, Error: "timeout waiting for apply"})
		case errors.Is(err, ErrKeyNotFound):
			writeJSON(w, http.StatusNotFound, jsonErr{OK: false, Error: "key not found"})
		default:
			writeJSON(w, http.StatusInternalServerError, jsonErr{OK: false, Error: err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, jsonGetOK{OK: true, Value: value})
}

func (s *HTTPServer) handleDebugNetworkPartition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, jsonErr{OK: false, Error: "method not allowed"})
		return
	}
	if s.partition == nil {
		writeJSON(w, http.StatusNotFound, jsonErr{OK: false, Error: "network partition toggle not configured"})
		return
	}
	raw := r.URL.Query().Get("isolated")
	if raw == "" {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "missing query isolated=true|false"})
		return
	}
	partitioned, err := strconv.ParseBool(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "isolated must be true or false"})
		return
	}
	s.partition.SetPartitioned(partitioned)
	if partitioned {
		s.service.Logger().Warn("Network partition simulated: incoming and outgoing Raft RPC are now DROPPED")
	} else {
		s.service.Logger().Warn("Network partition cleared: incoming and outgoing Raft RPC are now RESTORED")
	}
	writeJSON(w, http.StatusOK, jsonPutOK{OK: true})
}
