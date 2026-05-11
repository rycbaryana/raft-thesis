package network

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"raft-consensus/internal/raft"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNetworkPartition = errors.New("network partition: rpc dropped")
var ErrRPCTimeout = errors.New("rpc timeout")

var _ raft.RaftService = (*RPCClient)(nil)

const defaultRPCPoolSize = 4
const (
	initialDialBackoff = 25 * time.Millisecond
	maxDialBackoff     = 1 * time.Second
	tcpKeepAlive       = 30 * time.Second
)

type rpcConn struct {
	mu           sync.Mutex
	client       *rpc.Client
	dialFailures int
	retryAfter   time.Time
	lastDialErr  error
}

type RPCClient struct {
	address string
	timeout time.Duration
	pool    []rpcConn
	next    atomic.Uint64
}

func NewRPCClient(address string, timeout time.Duration) *RPCClient {
	if timeout <= 0 {
		timeout = time.Second
	}
	return &RPCClient{
		address: address,
		timeout: timeout,
		pool:    make([]rpcConn, defaultRPCPoolSize),
	}
}

func (c *RPCClient) pickConn() *rpcConn {
	if len(c.pool) == 0 {
		return nil
	}
	idx := c.next.Add(1) % uint64(len(c.pool))
	return &c.pool[idx]
}

func (c *RPCClient) getConnection(slot *rpcConn) (*rpc.Client, error) {
	slot.mu.Lock()
	defer slot.mu.Unlock()

	if slot.client != nil {
		return slot.client, nil
	}
	now := time.Now()
	if now.Before(slot.retryAfter) {
		if slot.lastDialErr != nil {
			return nil, slot.lastDialErr
		}
		return nil, fmt.Errorf("rpc dial backoff active for %s", c.address)
	}

	client, err := dialHTTPWithKeepAlive(c.address)
	if err != nil {
		slot.dialFailures++
		slot.retryAfter = now.Add(nextDialBackoff(slot.dialFailures))
		slot.lastDialErr = err
		return nil, err
	}
	slot.client = client
	slot.dialFailures = 0
	slot.retryAfter = time.Time{}
	slot.lastDialErr = nil
	return client, nil
}

func (c *RPCClient) resetConnection(slot *rpcConn) {
	if slot == nil {
		return
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()

	if slot.client != nil {
		slot.client.Close()
		slot.client = nil
	}
	// Avoid immediate reconnect storm after connection-level failures.
	slot.retryAfter = time.Now().Add(initialDialBackoff)
}

func (c *RPCClient) call(method string, args any, reply any, timeout time.Duration) error {
	slot := c.pickConn()
	if slot == nil {
		return errors.New("rpc pool is empty")
	}
	client, err := c.getConnection(slot)
	if err != nil {
		return err
	}

	call := client.Go(method, args, reply, nil)
	select {
	case <-call.Done:
		if call.Error != nil {
			c.resetConnection(slot)
			return call.Error
		}
		return nil

	case <-time.After(timeout):
		return ErrRPCTimeout
	}
}

func nextDialBackoff(failures int) time.Duration {
	if failures <= 0 {
		return initialDialBackoff
	}
	backoff := initialDialBackoff
	for i := 1; i < failures; i++ {
		backoff *= 2
		if backoff >= maxDialBackoff {
			return maxDialBackoff
		}
	}
	if backoff > maxDialBackoff {
		return maxDialBackoff
	}
	return backoff
}

func dialHTTPWithKeepAlive(address string) (*rpc.Client, error) {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: tcpKeepAlive,
	}
	conn, err := dialer.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(tcpKeepAlive)
	}

	_, err = io.WriteString(conn, "CONNECT "+rpc.DefaultRPCPath+" HTTP/1.0\n\n")
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("rpc connect failed: %s", resp.Status)
	}
	return rpc.NewClient(conn), nil
}

func (c *RPCClient) RequestVote(args *raft.RequestVoteArgs, reply *raft.RequestVoteReply) error {
	return c.call("Raft.RequestVote", args, reply, c.timeout)
}

func (c *RPCClient) AppendEntries(args *raft.AppendEntriesArgs, reply *raft.AppendEntriesReply) error {
	return c.call("Raft.AppendEntries", args, reply, c.timeout)
}
