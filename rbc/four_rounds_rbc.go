package rbc

import (
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/fri"
	"go.dedis.ch/kyber/v4"
	"hash"
	"sync"
)

type FourRoundRBC[M any] struct {
	RBC[[]byte]
	pred             func(M) bool
	marshalContent   func(M) ([]byte, error)
	unmarshalContent func([]byte) (M, error)
	hash.Hash
	echoCount int
	threshold int
	sentReady bool
	sync.RWMutex
	rsc *ReedSolomonCode
	kyber.Group
	m int
	k int
}

func NewFourRoundRBC[M any](pred func(M) bool, h hash.Hash, threshold int,
	marshal func(M) ([]byte, error),
	unmarshal func([]byte) (M, error)) *FourRoundRBC[M] {
	return &FourRoundRBC[M]{
		pred:             pred,
		marshalContent:   marshal,
		unmarshalContent: unmarshal,
		Hash:             h,
		threshold:        threshold,
		echoCount:        0,
		sentReady:        false,
		RWMutex:          sync.RWMutex{},
	}
}

func (frRbc *FourRoundRBC[M]) FreshHash(m []M) ([][]byte, error) {
	frRbc.Hash.Reset()
	mBytes, err := frRbc.marshalContent(m)
	if err != nil {
		return nil, err
	}

	for _, b := range mBytes {
		_, err = frRbc.Hash.Write(b)
		if err != nil {
			return nil, err
		}
	}
	h := frRbc.Hash.Sum(nil)
	return h, nil
}

func (frRbc *FourRoundRBC[M]) RBroadcast(m M) *RBCMessage[M] {
	return NewRBCMessage(PROPOSE, m)
}

func (frRbc *FourRoundRBC[M]) HandleMessage(msg RBCMessage[[]byte]) (*RBCMessage[[][]byte], bool) {
	switch msg.Type() {
	case PROPOSE:
		send := frRbc.receivePropose(msg.Content())
		if send {
			// Hash the value and return it in a message to be broadcast
			h, err := frRbc.FreshHash(val)
			if err != nil {
				return nil, false
			}
			Mp, err := frRbc.rsc.RSEnc(msg.Content(), frRbc.m, frRbc.Group)
			if err != nil {
				return nil, false
			}
			content := make([]byte, len(h)+len(Mp))
			copy(content, Mp)
			copy
			return NewRBCMessage(ECHO, h), true
		}
	case ECHO:
		send := frRbc.receiveEcho()
		if send {
			return NewRBCMessage(READY, msg.Content()), true
		}
	default:
		return nil, false
	}
	return nil, false
}

func (frRbc *FourRoundRBC[M]) receivePropose(m []byte) bool {
	val, err := frRbc.unmarshalContent(m)
	return err != nil && frRbc.pred(val)
}

func (frRbc *FourRoundRBC[M]) receiveEcho() bool {
	frRbc.RLock()
	defer frRbc.RUnlock()

	frRbc.echoCount++
	sendReady := frRbc.echoCount > 2*frRbc.threshold && !frRbc.sentReady
	if sendReady {
		frRbc.sentReady = true
	}
	// Return to send a ready message only if enough echo have been received and a ready has not been sent
	return sendReady
}

func (frRbc *FourRoundRBC[M]) receiveReady() bool {
	frRbc.RLock()
	defer frRbc.RUnlock()

}

func (frRbc *FourRoundRBC[M]) MarshalMessage() ([]byte, error) {
	// TODO
	return nil, nil
}

func (frRbc *FourRoundRBC[M]) UnmarshalMessage(b []byte) error {
	// TODO
	return nil
}
