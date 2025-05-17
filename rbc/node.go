package rbc

import (
	"context"
	"errors"
	"student_25_adkg/logging"
)

// Node listens to an network interface for byte messages and can send bytes on this network
type Node struct {
	nodeID           int64
	networkInterface AuthenticatedMessageStream
	logger           logging.Logger
	messageHandler   func([]byte) error
}

func NewNode(networkInterface AuthenticatedMessageStream, nodeID int64) *Node {
	return &Node{
		nodeID:           nodeID,
		networkInterface: networkInterface,
		logger:           logging.GetLogger(nodeID),
	}
}

func (n *Node) GetIdentifier() int64 {
	return n.nodeID
}

// SetCallback sets the function to be called when a message is received
// on the network
func (n *Node) SetCallback(handler func([]byte) error) {
	n.messageHandler = handler
}

// Start sets the node to listen for the network. When a message is received,
// the given handleMsg function is called
func (n *Node) Start(ctx context.Context) error {
	var returnErr error
	for returnErr == nil {
		bs, err := n.networkInterface.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				n.logger.Warn().Err(err).Msg("context canceled")
				returnErr = err
				continue
			}
			n.logger.Error().Err(err).Msg("error receiving message")
			continue
		}

		err = n.handleMessage(bs)
		if err != nil {
			n.logger.Err(err).Msg("error handling message")
			continue
		}
	}
	return returnErr
}

// handleMessage handles a message received on the network by passing it
// to the messageHandler if not nil
func (n *Node) handleMessage(bs []byte) error {
	if n.messageHandler == nil {
		n.logger.Warn().Msg("message handler is nil")
		return nil
	}

	return n.messageHandler(bs)
}

// BroadcastBytes sends the given byte message on the network
func (n *Node) BroadcastBytes(bs []byte) error {
	return n.networkInterface.Broadcast(bs)
}
