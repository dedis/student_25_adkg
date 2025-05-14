package bracha

import (
	"fmt"
	"os"
	"strconv"
	"student_25_adkg/rbc"
	"sync"
	"time"

	"github.com/rs/zerolog"
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
	*rbc.Handler[bool]
	config      *rbc.Config
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

	newRBC := &RBC{
		Node:        rbc.NewNode[Message](rbc.NodeIndex(nodeID), iface),
		Handler:     rbc.NewHandler[bool, Message](),
		predicate:   predicate,
		config:      &rbc.Config{Threshold: threshold},
		broadcaster: iface,
		RWMutex:     sync.RWMutex{},
		logger:      logger,
	}

	// RBC needs to be instantiated to have the handleMsg method
	newRBC.SetMessageHandler(newRBC.handleMsg)
	return newRBC
}

// RBroadcast implements the method from the RBC interface
func (b *RBC) RBroadcast(content bool) (rbc.InstanceIdentifier, error) {
	instanceID := rbc.InstanceIdentifier(b.GetIndex())
	// Start the instance by sending a PROPOSE message
	proposeMessage := NewBrachaMessage(instanceID, PROPOSE, content)
	err := b.SendMessage(proposeMessage)

	return instanceID, err
}

// createInstance creates a new RBC instance using the given identifier.
func (b *RBC) createInstance(identifier rbc.InstanceIdentifier) *Instance {
	// Create a new instance using the given identifierS
	return NewInstance(identifier, b.predicate, b.config)
}

// handleMsg handles a message received on the network
func (b *RBC) handleMsg(message *Message) error {
	instanceIdentifier := message.InstanceID
	_ = b.RegisterIfAbsent(b.createInstance(instanceIdentifier))
	toSend, err := b.HandleMessage(message)

	if err != nil || toSend == nil {
		return err
	}

	brachaMessage := toSend.(*Message)
	err = b.SendMessage(brachaMessage)
	if err != nil {
		return err
	}
	return nil
}
