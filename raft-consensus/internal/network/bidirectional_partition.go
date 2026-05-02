package network

// BidirectionalPartitionSwitch toggles full simulated network isolation on one node
// (no outgoing Raft RPC to peers and no incoming Raft RPC delivered to the core).
type BidirectionalPartitionSwitch struct {
	outgoing *OutgoingNetworkSwitch
	incoming *RaftRPCServerAdapter
}

func NewBidirectionalPartitionSwitch(out *OutgoingNetworkSwitch, in *RaftRPCServerAdapter) *BidirectionalPartitionSwitch {
	return &BidirectionalPartitionSwitch{outgoing: out, incoming: in}
}

// SetPartitioned enables (true) or clears (false) both directions at once.
func (s *BidirectionalPartitionSwitch) SetPartitioned(partitioned bool) {
	if partitioned {
		s.outgoing.DisconnectOutgoing()
		s.incoming.DisconnectIncoming()
		return
	}
	s.outgoing.ReconnectOutgoing()
	s.incoming.ReconnectIncoming()
}
