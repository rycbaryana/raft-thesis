package main

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"path/filepath"

	"raft-consensus/internal/kv"
	"raft-consensus/internal/network"
	"raft-consensus/internal/raft"
	"raft-consensus/internal/storage"
)

// nodeRuntime owns every component a node process needs to operate and shut down cleanly.
type nodeRuntime struct {
	cfg        *nodeConfig
	logger     *slog.Logger
	rf         *raft.Raft
	app        *kv.Service
	listener   net.Listener
	httpSrv    *http.Server
	diskCloser io.Closer
}

func buildNode(cfg *nodeConfig, logger *slog.Logger) (*nodeRuntime, error) {
	store, diskCloser, err := setupStorage(cfg)
	if err != nil {
		return nil, err
	}

	machine := kv.NewMachine(store)
	raftConfig := raft.DefaultProductionConfig()

	rf, partitionSwitch := setupRaftTransport(cfg, machine, raftConfig, logger)

	app := kv.NewService(rf, machine, logger)
	httpTransport := kv.NewHTTPServer(app, partitionSwitch)

	listener, httpSrv, err := setupHTTP(cfg, httpTransport, rf)
	if err != nil {
		if diskCloser != nil {
			_ = diskCloser.Close()
		}
		rf.Stop()
		return nil, err
	}

	return &nodeRuntime{
		cfg:        cfg,
		logger:     logger,
		rf:         rf,
		app:        app,
		listener:   listener,
		httpSrv:    httpSrv,
		diskCloser: diskCloser,
	}, nil
}

func setupStorage(cfg *nodeConfig) (storage.Store, io.Closer, error) {
	switch cfg.Storage {
	case "memory":
		return storage.NewMemoryStore(), nil, nil
	case "disk":
		if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create data dir: %w", err)
		}
		dbPath := filepath.Join(cfg.DataDir, fmt.Sprintf("node-%d.kvlog", cfg.ID))
		ds, err := storage.NewDiskStore(dbPath)
		if err != nil {
			return nil, nil, fmt.Errorf("init disk storage: %w", err)
		}
		return ds, ds, nil
	default:
		return nil, nil, fmt.Errorf("unsupported storage backend %q", cfg.Storage)
	}
}

func setupRaftTransport(cfg *nodeConfig, machine *kv.Machine, raftConfig raft.Config, logger *slog.Logger) (*raft.Raft, *network.BidirectionalPartitionSwitch) {
	outgoingSwitch := network.NewOutgoingNetworkSwitch()
	peerFactory := func(_ raft.NodeID, peerAddr string) raft.RaftService {
		inner := network.NewRPCClient(peerAddr, raftConfig.RPCTimeout)
		proxy := network.NewFaultInjectingRPCClient(inner)
		outgoingSwitch.AddProxy(proxy)
		return proxy
	}

	initialCluster := buildInitialCluster(cfg.ID, cfg.Addr, cfg.Bootstrap)
	rfLogger := logger.With(slog.String("component", "raft"))
	rf := raft.NewRaft(cfg.ID, machine, peerFactory, initialCluster,
		raft.WithLogger(rfLogger), raft.WithConfig(raftConfig))

	rpcAdapter := network.NewRaftRPCServerAdapter(rf)
	partitionSwitch := network.NewBidirectionalPartitionSwitch(outgoingSwitch, rpcAdapter)

	if err := rpc.RegisterName("Raft", rpcAdapter); err != nil {
		// RegisterName fails only on duplicate registration; in a single-process node this is fatal.
		panic(fmt.Errorf("register raft rpc: %w", err))
	}

	return rf, partitionSwitch
}

func setupHTTP(cfg *nodeConfig, httpTransport *kv.HTTPServer, _ *raft.Raft) (net.Listener, *http.Server, error) {
	mux := http.NewServeMux()
	mux.Handle("/", httpTransport.Handler())
	mux.Handle(rpc.DefaultRPCPath, rpc.DefaultServer)

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	return listener, &http.Server{Handler: mux}, nil
}

// Serve runs the HTTP+RPC server in the background. Returns immediately.
func (n *nodeRuntime) Serve() {
	go func() {
		if err := n.httpSrv.Serve(n.listener); err != nil && err != http.ErrServerClosed {
			n.logger.Error("http server error", "error", err)
		}
	}()
}

// Close shuts the node down in the correct order: HTTP server, raft, disk.
func (n *nodeRuntime) Close() {
	if n.httpSrv != nil {
		_ = n.httpSrv.Close()
	}
	if n.rf != nil {
		n.rf.Stop()
	}
	if n.diskCloser != nil {
		if err := n.diskCloser.Close(); err != nil {
			n.logger.Error("disk close failed", "error", err)
		}
	}
}
