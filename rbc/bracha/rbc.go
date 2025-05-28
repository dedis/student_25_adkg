package bracha

import (
	"context"
	"errors"
	"student_25_adkg/logging"
	"student_25_adkg/rbc"

	"github.com/rs/zerolog"
	"go.dedis.ch/protobuf"
)

// RBC implements Bracha RBC according to https://eprint.iacr.org/2021/777.pdf, algorithm 1.
type RBC struct {
	iface        rbc.AuthenticatedMessageStream
	predicate    func(bool) bool
	state        *State
	finishedChan chan rbc.Instance[bool]
	threshold    int
	nodeID       int64
	logger       zerolog.Logger
}

// NewBrachaRBC creates a new BrachaRBC structure
func NewBrachaRBC(predicate func(bool) bool, threshold int, iface rbc.AuthenticatedMessageStream, nodeID int64) *RBC {
	return &RBC{
		predicate:    predicate,
		iface:        iface,
		state:        NewState(1),
		finishedChan: make(chan rbc.Instance[bool]),
		threshold:    threshold,
		logger:       logging.GetLogger(nodeID),
		nodeID:       nodeID,
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

// RBroadcast implements the method from the RBC interface
func (b *RBC) RBroadcast(content bool) error {
	msg := NewBrachaMessage(PROPOSE, content)
	err := b.sendMsg(msg)
	return err
}

// Listen listens for packets on the interface and handles them. Run until the context
// is stopped or its deadline exceeded.
func (b *RBC) Listen(ctx context.Context) error {
	for {
		bs, err := b.iface.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				b.logger.Warn().Err(err).Msg("context canceled")
				failed := b.state.FailIfNotFinished()
				if failed {
					close(b.finishedChan)
				}
				return err
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
			b.logger.Err(err).Msg("Error handling message")
			continue
		}
	}
}

// HandleMsg implements the method from the RBC interface
func (b *RBC) handleMsg(message Message) error {
	send := false
	t := PROPOSE
	switch message.MsgType {
	case PROPOSE:
		send = b.receivePropose(message.Content)
		t = ECHO
	case ECHO:
		send = b.receiveEcho(message.Content)
		t = READY
	case READY:
		send = b.receiveReady(message.Content)
		t = READY
	default:
		send = false
	}

	if !send {
		return nil
	}

	toSend := NewBrachaMessage(t, message.Content)
	err := b.sendMsg(toSend)
	if err != nil {
		return err
	}
	if t == READY {
		b.state.SetSentReady()
	}

	return nil
}

// receivePropose handles the logic necessary when a PROPOSE message is received
func (b *RBC) receivePropose(s bool) (echo bool) {
	return b.predicate(s)
}

// receiveEcho handles the logic necessary when a ECHO message is received
func (b *RBC) receiveEcho(s bool) (ready bool) {
	if !b.predicate(s) || b.state.Finished() {
		return b.state.EchoCount() >= 2*b.threshold+1
	}
	echoCount := b.state.IncrementEchoCount()
	ready = checkEchoThreshold(echoCount, b.threshold) && !b.state.SentReady()
	return ready
}

// ReceiveReady handles the reception of a READY message. If enough ready messages have been received, the protocol
// returns finished=true and the value field is set. The ready bool specifies if a ready message should be sent
func (b *RBC) receiveReady(s bool) bool {
	if !b.predicate(s) || b.state.Finished() {
		return false
	}
	readyCount := b.state.IncrementReadyCount()
	ready := checkReadyThreshold(readyCount, b.threshold) && !b.state.SentReady()

	finished := readyCount > 2*b.threshold
	if finished {
		b.state.SetFinalValue(s)
		b.finishedChan <- b.state
	}

	return ready
}

func (b *RBC) GetFinishedChannel() <-chan rbc.Instance[bool] {
	return b.finishedChan
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
