package kv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
	mux.HandleFunc("/cluster/nodes", s.handleClusterNodes)
	mux.HandleFunc("/debug/network/partition", s.handleDebugNetworkPartition)
	return s.withRequestLog(mux)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (s *HTTPServer) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		} else if rec.status >= 400 {
			level = slog.LevelWarn
		}

		s.service.Logger().Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", time.Since(start),
		)
	})
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

type jsonAddClusterNodeReq struct {
	ID   int    `json:"id"`
	Addr string `json:"addr"`
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

func (s *HTTPServer) writeClusterMembershipErr(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, raft.ErrNotLeader):
		s.setLeaderHintHeader(w)
		writeJSON(w, http.StatusServiceUnavailable, jsonErr{OK: false, Error: "not leader"})
		return true
	case errors.Is(err, raft.ErrConfigChangeInProgress):
		writeJSON(w, http.StatusConflict, jsonErr{OK: false, Error: "configuration change already in progress"})
		return true
	case errors.Is(err, context.Canceled):
		writeJSON(w, statusClientClosed, jsonErr{OK: false, Error: "request canceled"})
		return true
	case errors.Is(err, context.DeadlineExceeded):
		writeJSON(w, http.StatusGatewayTimeout, jsonErr{OK: false, Error: "timeout waiting for commit"})
		return true
	case errors.Is(err, raft.ErrNodeNotInCluster):
		writeJSON(w, http.StatusNotFound, jsonErr{OK: false, Error: "node not in cluster"})
		return true
	case errors.Is(err, raft.ErrPeerAlreadyInCluster):
		writeJSON(w, http.StatusConflict, jsonErr{OK: false, Error: "node already in cluster"})
		return true
	default:
		return false
	}
}

func (s *HTTPServer) handleClusterNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleClusterAddNode(w, r)
	case http.MethodDelete:
		s.handleClusterRemoveNode(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodPost, http.MethodDelete}, ", "))
		writeJSON(w, http.StatusMethodNotAllowed, jsonErr{OK: false, Error: "method not allowed"})
	}
}

func (s *HTTPServer) handleClusterAddNode(w http.ResponseWriter, r *http.Request) {
	var req jsonAddClusterNodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "invalid JSON body"})
		return
	}
	if req.Addr == "" {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "missing addr"})
		return
	}
	if req.ID <= 0 {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "id must be positive"})
		return
	}

	ctx, cancel := s.requestContext(r)
	defer cancel()

	err := s.service.AddClusterNode(ctx, raft.NodeID(req.ID), req.Addr)
	if err != nil {
		if s.writeClusterMembershipErr(w, err) {
			return
		}
		writeJSON(w, http.StatusInternalServerError, jsonErr{OK: false, Error: err.Error()})
		return
	}

	s.service.Logger().Info("cluster node added via HTTP", "node_id", req.ID, "addr", req.Addr)
	writeJSON(w, http.StatusOK, jsonPutOK{OK: true})
}

func (s *HTTPServer) handleClusterRemoveNode(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("id")
	if raw == "" {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "missing query id"})
		return
	}
	id64, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id64 <= 0 {
		writeJSON(w, http.StatusBadRequest, jsonErr{OK: false, Error: "id must be a positive integer"})
		return
	}

	ctx, cancel := s.requestContext(r)
	defer cancel()

	if err := s.service.RemoveClusterNode(ctx, raft.NodeID(id64)); err != nil {
		if s.writeClusterMembershipErr(w, err) {
			return
		}
		writeJSON(w, http.StatusInternalServerError, jsonErr{OK: false, Error: err.Error()})
		return
	}

	s.service.Logger().Info("cluster node removed via HTTP", "node_id", id64)
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
