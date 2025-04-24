package bracha

import (
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"go.dedis.ch/protobuf"
	"os"
	"strconv"
	"student_25_adkg/rbc"
	"sync"
	"time"
)

// Define the logger
var (
	logout = zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		// Format the node ID
		FormatPrepare: func(e map[string]interface{}) error {
			e["id"] = fmt.Sprintf("[%s]", e["id"])
			return nil
		},
		// Change the order in which things appear
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.LevelFieldName,
			"id",
			zerolog.MessageFieldName,
		},
		// Prevent the id from being printed again
		FieldsExclude: []string{"id"},
	}
)

// BrachaRBC implements RBC according to https://eprint.iacr.org/2021/777.pdf, algorithm 1.
type BrachaRBC struct {
	iface      rbc.AuthenticatedMessageStream
	pred       func(bool) bool
	stopChan   chan struct{}
	echoCount  int
	readyCount int
	threshold  int
	sentReady  bool
	finished   bool
	value      bool
	id         uint32
	logger     zerolog.Logger
	sync.RWMutex
}

// NewBrachaRBC creates a new BrachaRBC structure. The marshal un unmarshal methods are used to convert the content
// of the RBCMessage to and from the value type M
func NewBrachaRBC(pred func(bool) bool, threshold int, iface rbc.AuthenticatedMessageStream, id uint32) *BrachaRBC {
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
		Str("id", strconv.Itoa(int(id))).
		Logger()

	return &BrachaRBC{
		pred:       pred,
		iface:      iface,
		stopChan:   make(chan struct{}),
		echoCount:  0,
		readyCount: 0,
		threshold:  threshold,
		sentReady:  false,
		finished:   false,
		RWMutex:    sync.RWMutex{},
		logger:     logger,
		id:         id,
	}
}

func (b *BrachaRBC) sendMsg(msg *BrachaMessage) error {
	marshalled, err := protobuf.Encode(msg)
	if err != nil {
		return err
	}
	err = b.iface.Broadcast(marshalled)
	return err
}

func (b *BrachaRBC) start(ctx context.Context, finishedChan chan struct{}) {
	go func() {
		for {
			bs, err := b.iface.Receive(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				b.logger.Error().Err(err).Msg("error receiving message")
				continue
			}
			msg := &BrachaMessage{}
			err = protobuf.Decode(bs, msg)
			if err != nil {
				b.logger.Error().Err(err).Msg("error decoding message")
				continue
			}
			err, finished := b.handleMsg(*msg)
			if err != nil {
				b.logger.Err(err).Msg("Error handling message")
				continue
			}
			if finished {
				b.finished = true
				b.value = msg.Content
				close(finishedChan)
			}
		}
	}()
}

// RBroadcast implements the method from the RBC interface
func (b *BrachaRBC) RBroadcast(ctx context.Context, content bool) error {
	finishedChan := make(chan struct{})
	b.start(ctx, finishedChan)

	err := b.startBroadcast(content)
	if err != nil {
		return err
	}

	// Wait for the protocol to finish
	<-finishedChan

	return nil
}

func (b *BrachaRBC) Listen(ctx context.Context) error {
	finishedChan := make(chan struct{})
	b.start(ctx, finishedChan)

	// Wait for the protocol to finish
	<-finishedChan

	return nil
}

func (b *BrachaRBC) startBroadcast(val bool) error {
	msg := NewBrachaMessage(PROPOSE, val)
	err := b.sendMsg(msg)
	return err
}

// HandleMsg implements the method from the RBC interface
func (b *BrachaRBC) handleMsg(message BrachaMessage) (error, bool) {
	send := false
	t := PROPOSE
	finished := false
	switch message.MsgType {
	case PROPOSE:
		send = b.receivePropose(message.Content)
		t = ECHO
	case ECHO:
		send = b.receiveEcho(message.Content)
		t = READY
	case READY:
		finished, send = b.receiveReady(message.Content)
		t = READY
	default:
		send = false
	}

	if send { // If there is something to send
		toSend := NewBrachaMessage(t, message.Content)
		err := b.sendMsg(toSend)
		if err != nil {
			return err, finished
		}
	}
	return nil, finished
}

// receivePropose handles the logic necessary when a PROPOSE message is received
func (b *BrachaRBC) receivePropose(s bool) (echo bool) {
	// If the predicate match, return that an echo message should be broadcast
	return b.pred(s)
}

// receiveEcho handles the logic necessary when a ECHO message is received
func (b *BrachaRBC) receiveEcho(s bool) (ready bool) {
	b.Lock()
	defer b.Unlock()
	// Don't do anything if the value doesn't match the predicate or the RBC protocol finished
	if !b.pred(s) || b.finished {
		return b.echoCount >= 2*b.threshold+1
	}
	// Increment the count of echo messages received
	b.echoCount++
	// Send a ready message if we have received 2t+1 ECHO messages and have not sent yet a ready message
	ready = b.echoCount > 2*b.threshold && !b.sentReady
	// If a ready message is going to be sent, set the sentReady bool to true to prevent sending more
	if ready {
		b.sentReady = true
	}
	return ready
}

// ReceiveReady handles the reception of a READY message. If enough ready messages have been received, the protocol
// returns finished=true and the value field is set. The ready bool specifies if a ready message should be sent
func (b *BrachaRBC) receiveReady(s bool) (finished bool, ready bool) {
	b.RLock()
	defer b.RUnlock()
	// Don't do anything if the value doesn't match the predicate of the RBC protocol finished
	if !b.pred(s) || b.finished {
		return false, false
	}
	// Increment the count of READY messages received
	b.readyCount++

	// Send a READY message if we have received enough READY messages and a READY message has not yet been sent by this node
	ready = b.readyCount > b.threshold && !b.sentReady
	// If ready, then mark that this node has sent a ready message
	if ready {
		b.sentReady = true
	}

	// Finish if we have received enough ready messages
	finished = b.readyCount > 2*b.threshold
	// if finishing, then set the value and the finished bool in the struct
	if finished {
		b.finished = true
		b.value = s
	}

	return finished, ready
}
