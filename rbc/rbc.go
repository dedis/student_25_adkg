package rbc

import (
	"context"
	"errors"
)

type AuthenticatedMessageBroadcaster interface {
	// Broadcast sends the given byte message to all nodes in the network.
	Broadcast([]byte) error
}

type AuthenticatedMessageReceiver interface {
	// Receive blocks until a message is received and returns this message or an error or the given channel
	// is return to. The channel is used to stop waiting for a message.
	Receive(context.Context) ([]byte, error)
}

// AuthenticatedMessageStream is an interface provided by the node to allow secure communication
// with the network on a specific stream of messages. I.e. if this is interface is used
// to start an RBC broadcast, all the message in the receiver should only contain messages related
// to this RBC instance.
type AuthenticatedMessageStream interface {
	AuthenticatedMessageBroadcaster
	AuthenticatedMessageReceiver
}

// RBC is an interface for an RBC protocol
type RBC[T any] interface {
	// RBroadcast broadcasts the given value and returns.
	RBroadcast(T) error
	// Listen makes the node listen to the network for RBC messages. This method returns only
	// when the context is stopped or its deadline exceeded returning any of the two errors.
	Listen(ctx context.Context) error
}

var ErrPredicateRejected = errors.New("predicate rejected")
