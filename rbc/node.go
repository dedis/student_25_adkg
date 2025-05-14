package rbc

import (
	"context"
	"errors"
	"student_25_adkg/logging"

	"github.com/rs/zerolog"
	"go.dedis.ch/protobuf"
)

type NodeIndex int64

// Node can broadcast message and listen to the underlying network for a broadcast
type Node[T interface{}] struct {
	nodeIndex        NodeIndex
	networkInterface AuthenticatedMessageStream
	messageHandler   func(*T) error
	logger           zerolog.Logger
}

func NewNode[T interface{}](index NodeIndex, networkInterface AuthenticatedMessageStream) *Node[T] {
	return &Node[T]{
		nodeIndex:        index,
		networkInterface: networkInterface,
		logger:           logging.GetLogger(int64(index)),
	}
}

func (n *Node[T]) SetMessageHandler(messageHandler func(*T) error) {
	n.messageHandler = messageHandler
}

// Start sets the node to listen for the network. When a message is received,
// the given handleMsg function is called
func (n *Node[T]) Start(ctx context.Context) error {
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
		var msg T
		err = protobuf.Decode(bs, &msg)
		if err != nil {
			n.logger.Error().Err(err).Msg("error decoding message")
			continue
		}
		err = n.messageHandler(&msg)
		if err != nil {
			n.logger.Err(err).Msg("error handling message")
			continue
		}
	}
	return returnErr
}

// GetIndex returns the index of this node
func (n *Node[T]) GetIndex() NodeIndex {
	return n.nodeIndex
}

func (n *Node[T]) SendMessage(msg Message) error {
	marshalled, err := protobuf.Encode(msg)
	if err != nil {
		return err
	}
	err = n.networkInterface.Broadcast(marshalled)
	return err
}
