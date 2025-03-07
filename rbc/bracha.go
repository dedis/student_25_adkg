package rbc

import (
	"errors"
	"go.dedis.ch/kyber/v4"
	"student_25_adkg/networking"
	"sync"
)

type BrachaRBC struct {
	pred       func(kyber.Scalar) bool
	echoCount  int
	readyCount int
	threshold  int
	sentReady  bool
	finished   bool
	value      kyber.Scalar
	sync.RWMutex
}

type BrachaMsgType int8

const (
	Propose BrachaMsgType = iota
	Echo
	Ready
)

type BrachaMsg networking.Message[BrachaMsgType, kyber.Scalar]

func NewBrachaMsg(t BrachaMsgType, s kyber.Scalar) *BrachaMsg {
	msg := networking.NewMessage[BrachaMsgType, kyber.Scalar](t, s)
	return (*BrachaMsg)(msg)
}

func NewBrachaRBC(pred func(scalar kyber.Scalar) bool, threshold int) *BrachaRBC {
	return &BrachaRBC{
		pred:       pred,
		echoCount:  0,
		readyCount: 0,
		threshold:  threshold,
		sentReady:  false,
		finished:   false,
		value:      nil,
		RWMutex:    sync.RWMutex{},
	}
}

func (rbc *BrachaRBC) Broadcast(s kyber.Scalar, iface networking.NetworkInterface[BrachaMsg]) error {
	if rbc.finished {
		return errors.New("BrachaRBC already finished")
	}
	return iface.Broadcast(*NewBrachaMsg(Propose, s))
}

func (rbc *BrachaRBC) ReceivePropose(s kyber.Scalar) (echo bool) {
	return rbc.pred(s)
}

func (rbc *BrachaRBC) BroadcastEcho(s kyber.Scalar, iface networking.NetworkInterface[BrachaMsg]) error {
	rbc.Lock()
	defer rbc.Unlock()
	if rbc.finished {
		return errors.New("RBC finished")
	}

	err := iface.Broadcast(*NewBrachaMsg(Echo, s))
	if err != nil {
		return err
	}
	rbc.sentReady = true
	return nil
}

func (rbc *BrachaRBC) ReceiveEcho(s kyber.Scalar) (ready bool) {
	rbc.Lock()
	defer rbc.Unlock()
	// Don't do anything if the value doesn't match the predicate of the RBC protocol finished
	if !rbc.pred(s) || rbc.finished {
		return rbc.echoCount >= 2*rbc.threshold+1
	}
	rbc.echoCount++
	// return true to tell the caller to broadcast a ready only if the threshold is attained and a ready has not yet been sent
	return rbc.echoCount > 2*rbc.threshold && !rbc.sentReady
}

func (rbc *BrachaRBC) BroadcastReady(s kyber.Scalar, iface networking.NetworkInterface[BrachaMsg]) error {
	rbc.RLock()
	defer rbc.RUnlock()
	// Don't do anything if the value doesn't match the predicate of the RBC protocol finished
	if !rbc.pred(s) || rbc.finished {
		return errors.New("message doesn't abide to the predicate")
	}
	err := iface.Broadcast(*NewBrachaMsg(Ready, s))
	if err != nil {
		return err
	}
	rbc.sentReady = true
	return nil
}

// ReceiveReady handles the reception of a ready message. If enough ready messages have been received, the protocol
// returns finished=true and the value of output can be read. If it is false, the value of output is to be ignored
// and the value of ready specifies if a ready message should be sent
func (rbc *BrachaRBC) ReceiveReady(s kyber.Scalar) (output kyber.Scalar, finished bool, ready bool) {
	rbc.RLock()
	defer rbc.RUnlock()
	// Don't do anything if the value doesn't match the predicate of the RBC protocol finished
	if !rbc.pred(s) || rbc.finished {
		return s, false, false
	}
	rbc.readyCount++
	if rbc.readyCount > 2*rbc.threshold {
		rbc.finished = true
		rbc.value = s
		return s, true, false
	}
	// Tell the caller to send a ready message if the threshold has been attained and a ready was not already sent
	return s, false, rbc.readyCount > rbc.threshold && !rbc.sentReady
}
