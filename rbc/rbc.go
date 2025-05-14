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
	// Receive blocks until a message is received. Returns this message or an error
	// If the context is canceled, returns context.Canceled error
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

type InstanceIdentifier int64

// Instance represents the process of a single message broadcast
// T is the type of message being broadcast
type Instance[T any] interface {
	GetIdentifier() InstanceIdentifier
	IsFinished() bool
	GetResult() (T, error)
}

// Broadcaster allows to reliably broadcast a message of type T
type Broadcaster[T any] interface {
	// RBroadcast reliably broadcasts the given value
	// If Stop is called, this method will return early without error.
	RBroadcast(context.Context, T) (InstanceIdentifier, error)
}

// Receiver allows to wait for an Instance of type T
type Receiver[T any] interface {
	// Receive blocks until an Instance is started and returns the instance or an error.
	// If an error occurs with th given instance, it will be returned as is.
	Receive(context.Context) (Instance[T], error)
}

// Node can broadcast message and listen to the underlying network for a broadcast
type Node[T any] interface {
	Broadcaster[T]
	Receiver[T]
	// GetIndex returns the index of this node
	GetIndex() int64
	// Start sets the node to listen for the network. This should be called before any broadcast
	Start(ctx context.Context) error
}

var ErrPredicateRejected = errors.New("predicate rejected")
var ErrInstanceNotFinished = errors.New("instance is not finished and thus no result has been produced")
