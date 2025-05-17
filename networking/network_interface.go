package networking

import "context"

// NetworkInterface represents an interface used by a node to communicate in the network
type NetworkInterface interface {
	// Send allows to send a byte message to a recipient addressed by an int
	Send([]byte, int64) error
	// Broadcast send the given byte message to everyone else in the network
	Broadcast([]byte) error
	// Receive waits on the channel for a message to arrive. Blocks until a message arrives or
	// the channel is written to. This allows stopping before receiving a message
	Receive(context.Context) ([]byte, error)
	GetID() int64
	GetSent() [][]byte
	GetReceived() [][]byte
}
