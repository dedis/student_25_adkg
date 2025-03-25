package rbc

import "student_25_adkg/messaging"

type MessageType int8

const (
	PROPOSE MessageType = iota
	ECHO
	READY
)

// RBCMessage contains a message type and a content of the specified type
type RBCMessage[M any] struct {
	messaging.Message[M]
	t       MessageType
	content M
}

// NewRBCMessage construct a new message specific to RBC
func NewRBCMessage[M any](t MessageType, content M) *RBCMessage[M] {
	return &RBCMessage[M]{
		t:       t,
		content: content,
	}
}

// Type returns the type of message
func (m *RBCMessage[M]) Type() MessageType {
	return m.t
}

// Content returns the content of the message
func (m *RBCMessage[M]) Content() M {
	return m.content
}

// RBC is an interface for a RBC protocol. M represents the type of the value that the protocol broadcasts
type RBC[M any] interface {
	// RBroadcast starts a reliable broadcast and returns the message to be broadcast
	RBroadcast(M) *RBCMessage[M]
	// HandleMessage handles a message received. It returns a message to broadcast. If it is nil, then nothing to do.
	// And a boolean indicating if the protocol finished
	HandleMessage(RBCMessage[M]) (*RBCMessage[M], bool)
	// MarshalMessage converts an RBCMessage to bytes. Error is non-nil if something went wrong
	MarshalMessage(RBCMessage[M]) ([]byte, error)
	// UnmarshalMessage converts a byte array to an RBCMessage. Error is non-nil if something went wrong
	UnmarshalMessage([]byte) (*RBCMessage[M], error)
}
