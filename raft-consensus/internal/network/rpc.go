package network

import (
	"errors"
	"net/rpc"
	"raft-consensus/internal/raft"
	"sync"
	"time"
)

var ErrNetworkPartition = errors.New("network partition: rpc dropped")
var ErrRPCTimeout = errors.New("rpc timeout")

var _ raft.RaftService = (*RPCClient)(nil)

type RPCClient struct {
	mu      sync.Mutex
	address string
	client  *rpc.Client
	timeout time.Duration
}

func NewRPCClient(address string, timeout time.Duration) *RPCClient {
	return &RPCClient{address: address, timeout: timeout}
}

func (c *RPCClient) getConnection() (*rpc.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return c.client, nil
	}

	client, err := rpc.DialHTTP("tcp", c.address)
	if err != nil {
		return nil, err
	}
	c.client = client
	return client, nil
}

func (c *RPCClient) resetConnection() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
}

func (c *RPCClient) call(method string, args any, reply any, timeout time.Duration) error {
	client, err := c.getConnection()
	if err != nil {
		return err
	}

	call := client.Go(method, args, reply, nil)
	select {
	case <-call.Done:
		if call.Error != nil {
			c.resetConnection()
			return call.Error
		}
		return nil

	case <-time.After(timeout):
		c.resetConnection()
		return ErrRPCTimeout
	}
}

func (c *RPCClient) RequestVote(args *raft.RequestVoteArgs, reply *raft.RequestVoteReply) error {
	return c.call("Raft.RequestVote", args, reply, c.timeout)
}

func (c *RPCClient) AppendEntries(args *raft.AppendEntriesArgs, reply *raft.AppendEntriesReply) error {
	return c.call("Raft.AppendEntries", args, reply, c.timeout)
}
