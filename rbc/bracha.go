package rbc

import (
	"sync"
)

// BrachaRBC implements RBC according to https://eprint.iacr.org/2021/777.pdf, algorithm 1.
type BrachaRBC[M any] struct {
	RBC[M]
	pred             func(M) bool
	echoCount        int
	readyCount       int
	threshold        int
	sentReady        bool
	finished         bool
	value            M
	marshalContent   func(M) ([]byte, error)
	unmarshalContent func([]byte) (M, error)
	sync.RWMutex
}

// NewBrachaRBC creates a new BrachaRBC structure. The marshal un unmarshal methods are used to convert the content
// of the RBCMessage to and from the value type M
func NewBrachaRBC[M any](pred func(M) bool, threshold int,
	marshall func(M) ([]byte, error),
	unmarshall func([]byte) (M, error)) *BrachaRBC[M] {
	return &BrachaRBC[M]{
		pred:             pred,
		echoCount:        0,
		readyCount:       0,
		threshold:        threshold,
		sentReady:        false,
		finished:         false,
		RWMutex:          sync.RWMutex{},
		marshalContent:   marshall,
		unmarshalContent: unmarshall,
	}
}

// RBroadcast implements the method from the RBC interface
func (rbc *BrachaRBC[M]) RBroadcast(content M) *RBCMessage[M] {
	return NewRBCMessage(PROPOSE, content)
}

// HandleMsg implements the method from the RBC interface
func (rbc *BrachaRBC[M]) HandleMsg(message RBCMessage[M]) (*RBCMessage[M], bool) {
	send := false
	t := PROPOSE
	finished := false
	switch message.Type() {
	case PROPOSE:
		send = rbc.receivePropose(message.Content())
		t = ECHO
	case ECHO:
		send = rbc.receiveEcho(message.Content())
		t = READY
	case READY:
		finished, send = rbc.receiveReady(message.Content())
		t = READY
	default:
		send = false
	}

	if send {
		return NewRBCMessage(t, message.Content()), finished
	}
	return nil, finished
}

// receivePropose handles the logic necessary when a PROPOSE message is received
func (rbc *BrachaRBC[M]) receivePropose(s M) (echo bool) {
	// If the predicate match, return that an echo message should be broadcast
	return rbc.pred(s)
}

// receiveEcho handles the logic necessary when a ECHO message is received
func (rbc *BrachaRBC[M]) receiveEcho(s M) (ready bool) {
	rbc.Lock()
	defer rbc.Unlock()
	// Don't do anything if the value doesn't match the predicate or the RBC protocol finished
	if !rbc.pred(s) || rbc.finished {
		return rbc.echoCount >= 2*rbc.threshold+1
	}
	// Increment the count of echo messages received
	rbc.echoCount++
	// Send a ready message if we have received 2t+1 ECHO messages and have not sent yet a ready message
	ready = rbc.echoCount > 2*rbc.threshold && !rbc.sentReady
	// If a ready message is going to be sent, set the sentReady bool to true to prevent sending more
	if ready {
		rbc.sentReady = true
	}
	return ready
}

// ReceiveReady handles the reception of a READY message. If enough ready messages have been received, the protocol
// returns finished=true and the value field is set. The ready bool specifies if a ready message should be sent
func (rbc *BrachaRBC[M]) receiveReady(s M) (finished bool, ready bool) {
	rbc.RLock()
	defer rbc.RUnlock()
	// Don't do anything if the value doesn't match the predicate of the RBC protocol finished
	if !rbc.pred(s) || rbc.finished {
		return false, false
	}
	// Increment the count of READY messages received
	rbc.readyCount++

	// Send a READY message if we have received enough READY messages and a READY message has not yet been sent by this node
	ready = rbc.readyCount > rbc.threshold && !rbc.sentReady
	// If ready, then mark that this node has sent a ready message
	if ready {
		rbc.sentReady = true
	}

	// Finish if we have received enough ready messages
	finished = rbc.readyCount > 2*rbc.threshold
	// if finishing, then set the value and the finished bool in the struct
	if finished {
		rbc.finished = true
		rbc.value = s
	}

	return finished, ready
}

// Marshal implements the marshal method from the RBC interface
func (rbc *BrachaRBC[M]) Marshal(msg *RBCMessage[M]) ([]byte, error) {
	// Make the type into a byte and concatenate with the content
	typeByte := make([]byte, 1)
	typeByte[0] = byte(msg.Type())
	contentByte, err := rbc.marshalContent(msg.Content())
	if err != nil {
		return nil, err
	}
	marshalled := make([]byte, 1+len(contentByte))
	marshalled[0] = typeByte[0]
	copy(marshalled[1:], contentByte)
	return marshalled, nil
}

// Unmarshal implements the unmarshal method from the RBC interface
func (rbc *BrachaRBC[M]) Unmarshal(data []byte) (*RBCMessage[M], error) {
	t := MessageType(data[0])
	content, err := rbc.unmarshalContent(data[1:])
	if err != nil {
		return nil, err
	}
	return NewRBCMessage(t, content), nil
}
