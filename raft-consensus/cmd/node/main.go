package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"raft-consensus/internal/kv"
	"raft-consensus/internal/network"
	"raft-consensus/internal/raft"
	"raft-consensus/internal/storage"
)

var peerConfig = map[raft.NodeID]string{
	1: "localhost:8081",
	2: "localhost:8082",
	3: "localhost:8083",
}

func getLogger(logLevelFlag *string) *slog.Logger {
	var logLevel slog.Level
	switch *logLevelFlag {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
}

func main() {
	// 1. Parse command line arguments
	// Usage: go run cmd/node/main.go -id 1 -log-level debug
	idFlag := flag.Int("id", 0, "Current Node ID (1, 2, or 3)")
	logLevelFlag := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	storageFlag := flag.String("storage", "memory", "Storage backend (memory|disk)")
	dataDirFlag := flag.String("data-dir", "data", "Data directory for disk storage")
	flag.Parse()

	id := raft.NodeID(*idFlag)
	addr, exists := peerConfig[id]
	if !exists {
		log.Fatalf("Invalid ID %d. Available IDs: 1, 2, 3", id)
	}

	raftConfig := raft.DefaultTestingConfig()

	// 2. Initialize Peer Access (fault-injecting proxies wrap real RPC clients)
	peers := make(map[raft.NodeID]raft.RaftService)
	var faultProxies []*network.FaultInjectingRPCClient
	for peerID, peerAddr := range peerConfig {
		if peerID == id {
			continue
		}
		inner := network.NewRPCClient(peerAddr, raftConfig.RPCTimeout)
		proxy := network.NewFaultInjectingRPCClient(inner)
		faultProxies = append(faultProxies, proxy)
		peers[peerID] = proxy
	}
	outgoingSwitch := network.NewOutgoingNetworkSwitch(faultProxies...)

	// 3. Compose storage -> kv machine -> raft -> kv service -> http.
	logger := getLogger(logLevelFlag)

	var (
		store      storage.Store
		diskCloser *storage.DiskStore
	)
	switch *storageFlag {
	case "memory":
		store = storage.NewMemoryStore()
	case "disk":
		if err := os.MkdirAll(*dataDirFlag, 0o755); err != nil {
			log.Fatalf("failed to create data dir: %v", err)
		}
		dbPath := filepath.Join(*dataDirFlag, fmt.Sprintf("node-%d.kvlog", id))
		ds, err := storage.NewDiskStore(dbPath)
		if err != nil {
			log.Fatalf("failed to init disk storage: %v", err)
		}
		store = ds
		diskCloser = ds
	default:
		log.Fatalf("unsupported storage backend %q, expected memory|disk", *storageFlag)
	}

	machine := kv.NewMachine(store)
	rfLogger := logger.With(slog.String("component", "raft"))
	rf := raft.NewRaft(id, peers, machine, raft.WithLogger(rfLogger), raft.WithConfig(raftConfig))
	rpcAdapter := network.NewRaftRPCServerAdapter(rf)
	partitionSwitch := network.NewBidirectionalPartitionSwitch(outgoingSwitch, rpcAdapter)
	app := kv.NewService(rf, machine, logger)
	httpTransport := kv.NewHTTPServer(app, partitionSwitch)

	if err := rpc.RegisterName("Raft", rpcAdapter); err != nil {
		log.Fatalf("failed to register raft rpc: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", httpTransport.Handler())
	mux.Handle(rpc.DefaultRPCPath, rpc.DefaultServer)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	httpServer := &http.Server{Handler: mux}
	go func() {
		if serveErr := httpServer.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Error("http server error", "error", serveErr)
		}
	}()

	log.Printf("---------------------------------------")
	log.Printf("NODE %d STARTED at %s", id, addr)
	log.Printf("Waiting for peers...")
	log.Printf("---------------------------------------")

	// 5. Keep main alive until Signal (Ctrl+C)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down node...")
	app.Stop()
	rf.Stop()
	if diskCloser != nil {
		if err := diskCloser.Close(); err != nil {
			logger.Error("disk close failed", "error", err)
		}
	}
}
