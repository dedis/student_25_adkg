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
	// RBroadcast blocks until the protocol is finished or an error occurred. The returned bool reflects this results
	// If Stop is called, this method will return early without error.
	RBroadcast(context.Context, T) error
	// Listen expects to receive a PROPOSE message at some point that will start the protocol. This method
	// blocks until the protocol is finished or an error is returned.
	// If Stop is called, this method will return early without error.
	Listen(ctx context.Context) error
}

// NodeStoppedError is used when the Listen or RBroadcast methods are stopped via
// the Stop method.
type NodeStoppedError struct{}

func (err NodeStoppedError) Error() string {
	return "node stopped"
}
func (err NodeStoppedError) Is(target error) bool {
	var nodeStoppedError NodeStoppedError
	ok := errors.As(target, &nodeStoppedError)
	return ok
}
