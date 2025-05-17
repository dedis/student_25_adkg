package rbc

import (
	"context"
	"errors"
)

var ErrNotFinished = errors.New("instance is not finished")
var ErrUnknownInstance = errors.New("instance identifier is unknown")

// Instance is handles the RBC protocol of a single broadcast value of type R.
// This instance can be identified, has a Resulting and a specific
// Implementation of RBC
type Instance[R any] interface {
	Resulting[R]
	Implementation[R]
	GetIdentifier() int64
}

// Implementation is a specific implementation of RBC (e.g. Bracha
// or 4Rounds)
type Implementation[R any] interface {
	// HandleMessage handles a message from this protocol and
	// returns a message to send as a response or nil if nothing needs
	// to be sent.
	HandleMessage(Message[R]) (Message[R], error)
}

// AuthenticatedMessageBroadcaster can broadcast bytes on an authenticated network
type AuthenticatedMessageBroadcaster interface {
	// Broadcast sends the given byte message to all nodes in the network.
	Broadcast([]byte) error
}

// AuthenticatedMessageReceiver can receive bytes on an authenticated network
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

// RBC runs multiple Instance for values of type R
type RBC[R any] interface {
	// RBroadcast broadcast the given value resulting in a new instance
	// of which the identifier is returned or an error.
	RBroadcast(R) (int64, error)
	GetInstance(int64) (Instance[R], error)
}

type Message[R any] interface {
	GetContent() R
}
