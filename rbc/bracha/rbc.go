package bracha

import (
	"context"
	"errors"
	"student_25_adkg/logging"
	"student_25_adkg/rbc"
	"sync"

	"github.com/rs/zerolog"
	"go.dedis.ch/protobuf"
)

// RBC implements Bracha RBC according to https://eprint.iacr.org/2021/777.pdf, algorithm 1.
type RBC struct {
	iface      rbc.AuthenticatedMessageStream
	predicate  func(bool) bool
	stopChan   chan struct{}
	echoCount  int
	readyCount int
	threshold  int
	sentReady  bool
	finished   bool
	value      bool
	nodeID     int64
	logger     zerolog.Logger
	sync.RWMutex
}

// NewBrachaRBC creates a new BrachaRBC structure
func NewBrachaRBC(predicate func(bool) bool, threshold int, iface rbc.AuthenticatedMessageStream, nodeID int64) *RBC {
	return &RBC{
		predicate:  predicate,
		iface:      iface,
		stopChan:   make(chan struct{}),
		echoCount:  0,
		readyCount: 0,
		threshold:  threshold,
		sentReady:  false,
		finished:   false,
		RWMutex:    sync.RWMutex{},
		logger:     logging.GetLogger(nodeID),
		nodeID:     nodeID,
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

// start listens for packets on the interface and handles them. Returns a channel that will
// return nil when the protocol finishes or an error if it stopped or any other reason
func (b *RBC) start(ctx context.Context) chan error {
	finishedChan := make(chan error)
	go func() {
		for {
			bs, err := b.iface.Receive(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					b.logger.Warn().Err(err).Msg("context canceled")
					finishedChan <- err
					return
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
			finished, err := b.handleMsg(*msg)
			if err != nil {
				b.logger.Err(err).Msg("Error handling message")
				continue
			}
			if finished {
				b.finished = true
				b.value = msg.Content
				finishedChan <- nil
				b.logger.Info().Msg("Protocol finished")
				return
			}
		}
	}()
	return finishedChan
}

// RBroadcast implements the method from the RBC interface
func (b *RBC) RBroadcast(ctx context.Context, content bool) error {
	finishedChan := b.start(ctx)

	err := b.startBroadcast(content)
	if err != nil {
		return err
	}

	// Wait for the protocol to finish
	err = <-finishedChan
	close(finishedChan)
	return err
}

func (b *RBC) Listen(ctx context.Context) error {
	finishedChan := b.start(ctx)

	// Wait for the protocol to finish
	err := <-finishedChan
	close(finishedChan)
	return err
}

func (b *RBC) startBroadcast(val bool) error {
	msg := NewBrachaMessage(PROPOSE, val)
	err := b.sendMsg(msg)
	return err
}

// HandleMsg implements the method from the RBC interface
func (b *RBC) handleMsg(message Message) (bool, error) {
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
			return finished, err
		}
		if t == READY {
			b.sentReady = true
		}
	}
	return finished, nil
}

// receivePropose handles the logic necessary when a PROPOSE message is received
func (b *RBC) receivePropose(s bool) (echo bool) {
	return b.predicate(s)
}

// receiveEcho handles the logic necessary when a ECHO message is received
func (b *RBC) receiveEcho(s bool) (ready bool) {
	b.Lock()
	defer b.Unlock()
	if !b.predicate(s) || b.finished {
		return b.echoCount >= 2*b.threshold+1
	}
	b.echoCount++
	ready = checkEchoThreshold(b.echoCount, b.threshold) && !b.sentReady
	return ready
}

// ReceiveReady handles the reception of a READY message. If enough ready messages have been received, the protocol
// returns finished=true and the value field is set. The ready bool specifies if a ready message should be sent
func (b *RBC) receiveReady(s bool) (finished bool, ready bool) {
	b.RLock()
	defer b.RUnlock()
	if !b.predicate(s) || b.finished {
		return false, false
	}
	b.readyCount++
	ready = checkReadyThreshold(b.readyCount, b.threshold) && !b.sentReady

	finished = b.readyCount > 2*b.threshold
	if finished {
		b.finished = true
		b.value = s
	}

	return finished, ready
}

// checkReadyThreshold checks if the number of READY messages has
// reached the required threshold
func checkReadyThreshold(readyCount, threshold int) bool {
	return readyCount > threshold
}

// checkEchoThreshold checks if the number of ECHO messages has
// reached the required threshold
func checkEchoThreshold(echoCount, threshold int) bool {
	return echoCount > 2*threshold
}
