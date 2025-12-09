package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/signal"
	"syscall"

	"raft-consensus/internal/network"
	"raft-consensus/internal/raft"
)

var peerConfig = map[raft.NodeID]string{
	1: "localhost:8081",
	2: "localhost:8082",
	3: "localhost:8083",
}

func main() {
	// 1. Parse command line arguments
	// Usage: go run cmd/node/main.go -id 1
	idFlag := flag.Int("id", 0, "Current Node ID (1, 2, or 3)")
	flag.Parse()

	id := raft.NodeID(*idFlag)
	addr, exists := peerConfig[id]
	if !exists {
		log.Fatalf("Invalid ID %d. Available IDs: 1, 2, 3", id)
	}

	// 2. Initialize Peer Access
	peers := make(map[raft.NodeID]raft.RaftService)
	for peerID, peerAddr := range peerConfig {
		if peerID == id {
			continue
		}
		peers[peerID] = network.NewRPCClient(peerAddr)
	}

	applyCh := make(chan raft.ApplyMsg)
	// 3. Create the Raft Node instance
	rf := raft.NewRaft(id, peers, applyCh)

	// 4. Register the RPC Server
	// This exposes the methods "Raft.RequestVote" and "Raft.AppendEntries"
	// over the network so other nodes can call them.
	err := rpc.RegisterName("Raft", rf)
	if err != nil {
		log.Fatal("Error registering RPC:", err)
	}
	rpc.HandleHTTP()

	// 5. Start TCP Listener
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal("Listen error:", err)
	}

	log.Printf("---------------------------------------")
	log.Printf("NODE %d STARTED at %s", id, addr)
	log.Printf("Waiting for peers...")
	log.Printf("---------------------------------------")

	http.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		cmd := r.URL.Query().Get("cmd")
		if cmd == "" {
			http.Error(w, "missing command", http.StatusBadRequest)
			return
		}

		isLeader, index, term := rf.Submit(cmd)
		if !isLeader {
			http.Error(w, "Not a leader", http.StatusServiceUnavailable)
			return
		}

		fmt.Fprintf(w, "Command '%s' accepted at index %d (Term %d)", cmd, index, term)
	})

	go func() {
		for msg := range applyCh {
			if msg.CommandValid {
				log.Printf(">>> [STATE MACHINE] Applied Command: %v <<<", msg.Command)
			}
		}
	}()

	// Start serving HTTP requests in a background goroutine
	go http.Serve(listener, nil)

	// 6. Keep main alive until Signal (Ctrl+C)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down node...")
}
