package bracha

import (
	"student_25_adkg/rbc"
	"sync"
)

// Instance represents an instance of an RBC protocol
type Instance struct {
	*State
	*Config
	predicate func(bool) bool
	id        rbc.InstanceIdentifier
	result    bool
	finished  bool
	finishedC chan bool
	sync.RWMutex
}

func NewInstance(id rbc.InstanceIdentifier, predicate func(bool) bool, config *Config) *Instance {
	return &Instance{
		id:        id,
		predicate: predicate,
		finished:  false,
		finishedC: make(chan bool),
		result:    false,
		RWMutex:   sync.RWMutex{},
		State:     NewState(),
		Config:    config,
	}
}

func (i *Instance) GetIdentifier() rbc.InstanceIdentifier {
	return i.id
}

func (i *Instance) GetResult() (bool, error) {
	i.RLock()
	defer i.RUnlock()
	if !i.finished {
		return i.result, rbc.ErrInstanceNotFinished
	}
	return i.result, nil
}

func (i *Instance) IsFinished() bool {
	i.RLock()
	defer i.RUnlock()
	return i.finished
}

func (i *Instance) getFinishedChan() <-chan bool {
	return i.finishedC
}

func (i *Instance) setFinished(result bool) {
	i.finished = true
	i.result = result
	close(i.finishedC)
}

// HandleMessage handles a message received on the network
func (i *Instance) HandleMessage(message rbc.Message) rbc.Message {
	brachaMessage := message.(*Message)
	send := false
	messageType := PROPOSE
	switch brachaMessage.MsgType {
	case PROPOSE:
		send = i.receivePropose(brachaMessage.Content)
		messageType = ECHO
	case ECHO:
		send = i.receiveEcho(brachaMessage.Content)
		messageType = READY
	case READY:
		send = i.receiveReady(brachaMessage.Content)
		messageType = READY
	default:
		send = false
	}

	if !send {
		return nil
	}

	toSend := NewBrachaMessage(brachaMessage.InstanceID, messageType, brachaMessage.Content)
	if toSend != nil && messageType == READY {
		i.State.SetSentReady(true)
	}
	return toSend
}

// receivePropose handles the logic necessary when a PROPOSE message is received
func (i *Instance) receivePropose(s bool) (echo bool) {
	return i.predicate(s)
}

// receiveEcho handles the logic necessary when a ECHO message is received
func (i *Instance) receiveEcho(s bool) (ready bool) {
	i.Lock()
	defer i.Unlock()
	if !i.predicate(s) || i.finished {
		return i.checkEchoThreshold()
	}
	i.echoCount.Inc()
	ready = i.checkEchoThreshold() && !i.readySent
	return ready
}

// ReceiveReady handles the reception of a READY message. If enough ready messages have been received, the protocol
// returns finished=true and the value field is set. The ready bool specifies if a ready message should be sent
func (i *Instance) receiveReady(value bool) (ready bool) {
	i.RLock()
	defer i.RUnlock()
	if !i.predicate(value) || i.finished {
		return false
	}
	i.readyCount.Inc()
	ready = i.checkReadyThreshold() && !i.readySent

	if i.checkFinished() {
		i.setFinished(value)
	}

	return ready
}

func (i *Instance) checkEchoThreshold() bool {
	return i.echoCount.Value() >= 2*i.threshold
}

// checkReadyThreshold checks if the number of READY messages has
// reached the required threshold
func (i *Instance) checkReadyThreshold() bool {
	return i.GetReadyCount() > i.threshold
}

// checkFinished checks if enough ready messages have been received to finish the protocol
func (i *Instance) checkFinished() bool {
	return i.GetReadyCount() > 2*i.threshold
}
