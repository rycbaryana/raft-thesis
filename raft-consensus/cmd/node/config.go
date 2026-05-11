package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"raft-consensus/internal/raft"
)

const defaultPeerPortOffset = 8080

type nodeConfig struct {
	ID        raft.NodeID
	Addr      string
	Bootstrap []raft.NodeID
	LogLevel  string
	Storage   string
	DataDir   string
}

func defaultRPCAddr(id raft.NodeID) string {
	return fmt.Sprintf("127.0.0.1:%d", defaultPeerPortOffset+id)
}

func parseFlags() (*nodeConfig, error) {
	idFlag := flag.Int("id", 0, "This node's Raft ID (positive integer; listen port default 8080+id)")
	addrFlag := flag.String("addr", "", "This node's host:port for RPC/HTTP (default: 127.0.0.1:8080+id)")
	bootstrapFlag := flag.String("bootstrap", "", "Comma-separated founding voter IDs, e.g. 1,2,3. Set ONLY for the very first start of the founding members; joining nodes leave this empty.")
	logLevelFlag := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	storageFlag := flag.String("storage", "memory", "Storage backend (memory|disk)")
	dataDirFlag := flag.String("data-dir", "data", "Data directory for disk storage")
	flag.Parse()

	if *idFlag <= 0 {
		return nil, fmt.Errorf("-id is required and must be a positive node id (e.g. -id 4)")
	}
	id := raft.NodeID(*idFlag)

	addr := strings.TrimSpace(*addrFlag)
	if addr == "" {
		addr = defaultRPCAddr(id)
	}

	bootstrap, err := parseBootstrapIDs(*bootstrapFlag)
	if err != nil {
		return nil, fmt.Errorf("-bootstrap: %w", err)
	}

	switch *storageFlag {
	case "memory", "disk":
	default:
		return nil, fmt.Errorf("unsupported storage backend %q, expected memory|disk", *storageFlag)
	}

	return &nodeConfig{
		ID:        id,
		Addr:      addr,
		Bootstrap: bootstrap,
		LogLevel:  *logLevelFlag,
		Storage:   *storageFlag,
		DataDir:   *dataDirFlag,
	}, nil
}

func parseBootstrapIDs(s string) ([]raft.NodeID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	seen := make(map[raft.NodeID]struct{})
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid peer id %q", part)
		}
		seen[raft.NodeID(n)] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("no valid ids parsed")
	}
	out := make([]raft.NodeID, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out, nil
}

func buildInitialCluster(self raft.NodeID, selfAddr string, bootstrap []raft.NodeID) map[raft.NodeID]string {
	if len(bootstrap) == 0 {
		return nil
	}
	m := make(map[raft.NodeID]string, len(bootstrap))
	for _, id := range bootstrap {
		if id == self {
			m[id] = selfAddr
		} else {
			m[id] = defaultRPCAddr(id)
		}
	}
	return m
}
