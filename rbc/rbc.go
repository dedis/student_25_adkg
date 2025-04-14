package rbc

type InstanceID uint64

type AuthenticatedMessageBroadcaster interface {
	// Broadcast sends the given byte message to all nodes in the network.
	Broadcast([]byte) error
}

type AuthenticatedMessageReceiverHandler interface {
	// Receive blocks until a message is received and returns this message or an error
	// if anything bad happened
	Receive() ([]byte, error)
}

// AuthenticatedMessageStream is an interface provided by the node to allow secure communication
// with the network on a specific stream of messages. I.e. if this is interface is used
// to start an RBC broadcast, all the message in the receiver should only contain messages related
// to this RBC instance.
type AuthenticatedMessageStream interface {
	AuthenticatedMessageBroadcaster
	AuthenticatedMessageReceiverHandler
}

// RBC is an interface for an RBC protocol. M represents the type of the value that the protocol broadcasts
type RBC[M any] interface {
	// RBroadcast blocks until the protocol is finished or an error occurred. The returned bool reflects this results
	RBroadcast(M) error
	// Listen expects to receive a PROPOSE message at some point that will start the protocol. This method
	// blocks until the protocol is finished or an error is returned.
	Listen() error
}
