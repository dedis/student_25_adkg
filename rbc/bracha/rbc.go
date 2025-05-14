package bracha

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"student_25_adkg/rbc"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.dedis.ch/protobuf"
)

// Define the logger
var (
	logout = zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		// Format the node ID
		FormatPrepare: func(e map[string]interface{}) error {
			e["nodeID"] = fmt.Sprintf("[%s]", e["nodeID"])
			return nil
		},
		// Change the order in which things appear
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.LevelFieldName,
			"nodeID",
			zerolog.MessageFieldName,
		},
		// Prevent the nodeID from being printed again
		FieldsExclude: []string{"nodeID"},
	}
)

type Config struct {
	threshold int
}

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

// RBC implements Bracha RBC according to https://eprint.iacr.org/2021/777.pdf, algorithm 1.
type RBC struct {
	config    *Config
	iface     rbc.AuthenticatedMessageStream
	predicate func(bool) bool
	instances map[rbc.InstanceIdentifier]*Instance
	nodeID    int64
	logger    zerolog.Logger
	sync.RWMutex
}

// NewBrachaRBC creates a new BrachaRBC structure
func NewBrachaRBC(predicate func(bool) bool, threshold int, iface rbc.AuthenticatedMessageStream, nodeID int64) *RBC {
	// Disable logging based on the GLOG environment variable
	var logLevel zerolog.Level
	if os.Getenv("GLOG") == "no" {
		logLevel = zerolog.Disabled
	} else {
		logLevel = zerolog.InfoLevel
	}

	logger := zerolog.New(logout).
		Level(logLevel).
		With().
		Timestamp().
		Str("nodeID", strconv.Itoa(int(nodeID))).
		Logger()

	return &RBC{
		predicate: predicate,
		config:    &Config{threshold: threshold},
		iface:     iface,
		instances: make(map[rbc.InstanceIdentifier]*Instance),
		RWMutex:   sync.RWMutex{},
		logger:    logger,
		nodeID:    nodeID,
	}
}

func (b *RBC) sendMsg(msg *Message) error {
	marshalled, err := protobuf.Encode(msg)
	if err != nil {
		return err
	}
	err = b.iface.Broadcast(marshalled)
	return err
}

// Start listens for packets on the interface and handles them. Blocks until the given context is canceled
func (b *RBC) Start(ctx context.Context) error {
	var returnErr error
	for returnErr == nil {
		bs, err := b.iface.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				b.logger.Warn().Err(err).Msg("context canceled")
				returnErr = err
				continue
			}
			b.logger.Error().Err(err).Msg("error receiving message")
			continue
		}
		msg := &Message{}
		err = protobuf.Decode(bs, msg)
		if err != nil {
			b.logger.Error().Err(err).Msg("error decoding message")
			continue
		}
		err = b.handleMsg(*msg)
		if err != nil {
			b.logger.Err(err).Msg("error handling message")
			continue
		}
	}
	return returnErr
}

var ErrAlreadyRunningBroadcast = errors.New("can't start a broadcast from this node because a broadcast is already running")

// RBroadcast implements the method from the RBC interface
func (b *RBC) RBroadcast(content bool) (rbc.InstanceIdentifier, error) {
	instanceID := rbc.InstanceIdentifier(b.nodeID)
	// Start the instance by sending a PROPOSE message
	err := b.sendProposeMessage(instanceID, content)

	return instanceID, err
}

func (b *RBC) GetInstance(id rbc.InstanceIdentifier) (*Instance, bool) {
	b.RLock()
	defer b.RUnlock()
	instance, ok := b.instances[id]
	return instance, ok

}

// sendProposeMessage creates a PROPOSE message using the given value and the instance identifier and
// broadcasts it using the underlying network
func (b *RBC) sendProposeMessage(instanceID rbc.InstanceIdentifier, val bool) error {
	msg := NewBrachaMessage(instanceID, PROPOSE, val)
	err := b.sendMsg(msg)
	return err
}

// createInstance creates a new RBC instance using the given identifier and registers it.
// returns ErrAlreadyRunningBroadcast if a broadcast with the given identifier is already running
func (b *RBC) createInstance(identifier rbc.InstanceIdentifier) (*Instance, error) {
	b.Lock()
	defer b.Unlock()
	if _, ok := b.instances[identifier]; ok {
		return nil, ErrAlreadyRunningBroadcast
	}

	// Create a new instance using the given identifierS
	instance := NewInstance(identifier, func(_ bool) bool { return true }, b.config) // TODO predicate from somewhere
	b.instances[identifier] = instance

	return instance, nil
}

// handleMsg handles a message received on the network
func (b *RBC) handleMsg(message Message) error {
	var err error
	instance := b.instances[message.InstanceID]
	send := false
	messageType := PROPOSE
	switch message.MsgType {
	case PROPOSE:
		// Create instance
		instance, err = b.createInstance(message.InstanceID)
		if err != nil {
			break
		}

		send = instance.receivePropose(message.Content)
		messageType = ECHO
	case ECHO:
		send = instance.receiveEcho(message.Content)
		messageType = READY
	case READY:
		send = instance.receiveReady(message.Content)
		messageType = READY
	default:
		send = false
	}

	if err != nil || !send {
		return err
	}

	toSend := NewBrachaMessage(message.InstanceID, messageType, message.Content)
	err = b.sendMsg(toSend)
	if err != nil {
		return err
	}
	if messageType == READY {
		instance.SetSentReady(true)
	}
	return nil
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
