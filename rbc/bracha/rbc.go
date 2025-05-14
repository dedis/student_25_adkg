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

// RBC implements Bracha RBC according to https://eprint.iacr.org/2021/777.pdf, algorithm 1.
type RBC struct {
	*rbc.Node[Message]
	handler     *rbc.Handler[bool]
	config      *Config
	broadcaster rbc.AuthenticatedMessageBroadcaster
	predicate   func(bool) bool
	logger      zerolog.Logger
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
		Node:        rbc.NewNode[Message](rbc.NodeIndex(nodeID), iface),
		handler:     rbc.NewHandler[bool, Message](),
		predicate:   predicate,
		config:      &Config{threshold: threshold},
		broadcaster: iface,
		RWMutex:     sync.RWMutex{},
		logger:      logger,
	}
}

func (b *RBC) sendMsg(msg *Message) error {
	marshalled, err := protobuf.Encode(msg)
	if err != nil {
		return err
	}
	err = b.broadcaster.Broadcast(marshalled)
	return err
}

// Start listens for packets on the interface and handles them. Blocks until the given context is canceled
func (b *RBC) Start(ctx context.Context) error {
	return b.Node.Start(ctx, b.handleMsg)
}

var ErrAlreadyRunningBroadcast = errors.New("can't start a broadcast from this node because a broadcast is already running")

// RBroadcast implements the method from the RBC interface
func (b *RBC) RBroadcast(content bool) (rbc.InstanceIdentifier, error) {
	instanceID := rbc.InstanceIdentifier(b.GetIndex())
	// Start the instance by sending a PROPOSE message
	err := b.sendProposeMessage(instanceID, content)

	return instanceID, err
}

func (b *RBC) GetInstance(id rbc.InstanceIdentifier) (*Instance, bool) {
	b.RLock()
	defer b.RUnlock()
	instance, err := b.handler.Get(id)
	return instance.(*Instance), err == nil
}

// sendProposeMessage creates a PROPOSE message using the given value and the instance identifier and
// broadcasts it using the underlying network
func (b *RBC) sendProposeMessage(instanceID rbc.InstanceIdentifier, val bool) error {
	msg := NewBrachaMessage(instanceID, PROPOSE, val)
	err := b.sendMsg(msg)
	return err
}

// createInstance creates a new RBC instance using the given identifier.
func (b *RBC) createInstance(identifier rbc.InstanceIdentifier) *Instance {
	// Create a new instance using the given identifierS
	return NewInstance(identifier, func(_ bool) bool { return true }, b.config) // TODO predicate from somewhere
}

// handleMsg handles a message received on the network
func (b *RBC) handleMsg(message *Message) error {
	instanceIdentifier := message.InstanceID
	_ = b.handler.RegisterIfAbsent(b.createInstance(instanceIdentifier))
	toSend, err := b.handler.HandleMessage(message)

	if err != nil || toSend == nil {
		return err
	}

	brachaMessage := toSend.(*Message)
	err = b.sendMsg(brachaMessage)
	if err != nil {
		return err
	}
	return nil
}
