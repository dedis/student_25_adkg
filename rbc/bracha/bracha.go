package bracha

import (
	"context"
	"go.dedis.ch/protobuf"
	"student_25_adkg/rbc"
	"sync"
)

// BrachaRBC implements RBC according to https://eprint.iacr.org/2021/777.pdf, algorithm 1.
type BrachaRBC struct {
	rbc.RBC[bool]
	iface      rbc.AuthenticatedMessageStream
	pred       func(bool) bool
	echoCount  int
	readyCount int
	threshold  int
	sentReady  bool
	finished   bool
	value      bool
	sync.RWMutex
}

// NewBrachaRBC creates a new BrachaRBC structure. The marshal un unmarshal methods are used to convert the content
// of the RBCMessage to and from the value type M
func NewBrachaRBC(pred func(bool) bool, threshold int, iface rbc.AuthenticatedMessageStream) *BrachaRBC {
	return &BrachaRBC{
		pred:       pred,
		iface:      iface,
		echoCount:  0,
		readyCount: 0,
		threshold:  threshold,
		sentReady:  false,
		finished:   false,
		RWMutex:    sync.RWMutex{},
	}
}

func (rbc *BrachaRBC) sendMsg(msg *BrachaMessage) error {
	marshalled, err := protobuf.Encode(msg)
	if err != nil {
		return err
	}
	err = rbc.iface.Broadcast(marshalled)
	return err
}

// RBroadcast implements the method from the RBC interface
func (rbc *BrachaRBC) RBroadcast(content bool) error {
	// Register a handler for messages received from that stream
	ctx, cancel := context.WithCancel(context.Background())
	rbc.iface.AddHandler(func(received []byte) error {
		msg := &BrachaMessage{}
		err := protobuf.Decode(received, msg)
		if err != nil {
			return err
		}
		err, finished := rbc.handleMsg(*msg)
		if finished {
			rbc.finished = true
			rbc.value = msg.Content
			cancel()
		}
		return nil
	})

	// Start

	err := rbc.startBroadcast(content)
	if err != nil {
		return err // TODO check this
	}

	// Now the handler registered above will take over the control flow, and we just need to wait for the context
	<-ctx.Done()

	return nil
}

func (rbc *BrachaRBC) Listen() error {
	ctx, cancel := context.WithCancel(context.Background())
	rbc.iface.AddHandler(func(received []byte) error {
		msg := &BrachaMessage{}
		err := protobuf.Decode(received, msg)
		if err != nil {
			return err
		}
		err, finished := rbc.handleMsg(*msg)
		if finished {
			rbc.finished = true
			rbc.value = msg.Content
			cancel()
		}
		return nil
	})

	// Now the handler registered above will take over the control flow, and we just need to wait for the context
	<-ctx.Done()

	return nil
}

func (rbc *BrachaRBC) startBroadcast(val bool) error {
	msg := NewBrachaMessage(PROPOSE, val)
	err := rbc.sendMsg(msg)
	return err
}

// HandleMsg implements the method from the RBC interface
func (rbc *BrachaRBC) handleMsg(message BrachaMessage) (error, bool) {
	send := false
	t := PROPOSE
	finished := false
	switch message.MsgType {
	case PROPOSE:
		send = rbc.receivePropose(message.Content)
		t = ECHO
	case ECHO:
		send = rbc.receiveEcho(message.Content)
		t = READY
	case READY:
		finished, send = rbc.receiveReady(message.Content)
		t = READY
	default:
		send = false
	}

	if send { // If there is something to send
		toSend := NewBrachaMessage(t, message.Content)
		err := rbc.sendMsg(toSend)
		if err != nil {
			return err, finished
		}
	}
	return nil, finished
}

// receivePropose handles the logic necessary when a PROPOSE message is received
func (rbc *BrachaRBC) receivePropose(s bool) (echo bool) {
	// If the predicate match, return that an echo message should be broadcast
	return rbc.pred(s)
}

// receiveEcho handles the logic necessary when a ECHO message is received
func (rbc *BrachaRBC) receiveEcho(s bool) (ready bool) {
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
func (rbc *BrachaRBC) receiveReady(s bool) (finished bool, ready bool) {
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
