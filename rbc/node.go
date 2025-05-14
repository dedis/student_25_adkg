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
	nodeIndex NodeIndex
	receiver  AuthenticatedMessageReceiver
	logger    zerolog.Logger
}

func NewNode[T interface{}](index NodeIndex, receiver AuthenticatedMessageReceiver) *Node[T] {
	return &Node[T]{
		nodeIndex: index,
		receiver:  receiver,
		logger:    logging.GetLogger(int64(index)),
	}
}

// Start sets the node to listen for the network. When a message is received,
// the given handleMsg function is called
func (n Node[T]) Start(ctx context.Context, handleMsg func(*T) error) error {
	var returnErr error
	for returnErr == nil {
		bs, err := n.receiver.Receive(ctx)
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
		err = handleMsg(&msg)
		if err != nil {
			n.logger.Err(err).Msg("error handling message")
			continue
		}
	}
	return returnErr
}

// GetIndex returns the index of this node
func (n Node[T]) GetIndex() NodeIndex {
	return n.nodeIndex
}
