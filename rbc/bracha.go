package rbc

import (
	"sync"
)

// Bracha's implementation of RBC
type BrachaRBC[M any] struct {
	RBC[M]
	pred       func(M) bool
	echoCount  int
	readyCount int
	threshold  int
	sentReady  bool
	finished   bool
	value      M
	sync.RWMutex
}

// NewBrachaRBC creates a new Bracha RBC structure
func NewBrachaRBC[M any](pred func(scalar M) bool, threshold int) *BrachaRBC[M] {
	return &BrachaRBC[M]{
		pred:       pred,
		echoCount:  0,
		readyCount: 0,
		threshold:  threshold,
		sentReady:  false,
		finished:   false,
		RWMutex:    sync.RWMutex{},
	}
}

// Deal implements the method from the RBC interface
func (rbc *BrachaRBC[M]) Deal(val M) (MessageType, M) {
	return PROPOSE, val
}

// HandleMsg implements the method from the RBC interface
func (rbc *BrachaRBC[M]) HandleMsg(msgType MessageType, val M) (MessageType, M, bool, M, bool) {
	switch msgType {
	case PROPOSE:
		echo := rbc.receivePropose(val)
		return ECHO, val, echo, val, false
	case ECHO:
		ready := rbc.receiveEcho(val)
		return READY, val, ready, val, false
	case READY:
		finished, ready := rbc.receiveReady(val)
		if finished {
			return msgType, val, false, val, true
		}
		return msgType, val, ready, val, false
	default:
		return msgType, val, false, val, false
	}
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
