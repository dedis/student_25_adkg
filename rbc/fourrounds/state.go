package fourrounds

import (
	"student_25_adkg/reedsolomon"
	"sync"
)

type State struct {
	sentReady       bool
	echoCount       int
	readyCount      int
	readyShares     map[*reedsolomon.Encoding]struct{}
	finalValue      []byte
	finished        bool
	success         bool
	messageHash     []byte
	passedPredicate bool
	sync.RWMutex
}

func NewState(messageHash []byte) *State {
	return &State{
		sentReady:       false,
		echoCount:       0,
		readyCount:      0,
		readyShares:     make(map[*reedsolomon.Encoding]struct{}),
		finalValue:      nil,
		finished:        false,
		success:         false,
		passedPredicate: false,
		messageHash:     messageHash,
		RWMutex:         sync.RWMutex{},
	}
}

func (s *State) EchoCount() int {
	s.RLock()
	defer s.RUnlock()
	return s.echoCount
}

func (s *State) ReadyCount() int {
	s.RLock()
	defer s.RUnlock()
	return s.readyCount
}

func (s *State) SentReady() bool {
	s.RLock()
	defer s.RUnlock()
	return s.sentReady
}

func (s *State) ReadyShares() []*reedsolomon.Encoding {
	shares := make([]*reedsolomon.Encoding, 0, len(s.readyShares))
	for share := range s.readyShares {
		shares = append(shares, share)
	}
	return shares
}

func (s *State) GetValue() []byte {
	s.RLock()
	defer s.RUnlock()
	return s.finalValue
}

func (s *State) Finished() bool {
	s.RLock()
	defer s.RUnlock()
	return s.finished
}

func (s *State) Success() bool {
	s.RLock()
	defer s.RUnlock()
	return s.success
}

func (s *State) Identifier() []byte {
	s.RLock()
	defer s.RUnlock()
	return s.messageHash
}

func (s *State) PredicatePassed() bool {
	s.RLock()
	defer s.RUnlock()
	return s.passedPredicate
}

func (s *State) SetPredicatePassed() {
	s.Lock()
	defer s.Unlock()
	s.passedPredicate = true
}

// SetSentReady set the sentReady variable to true and returns
// whether it was already true or not
func (s *State) SetSentReady() (alreadyTrue bool) {
	s.Lock()
	defer s.Unlock()
	if s.sentReady {
		return true
	}
	s.sentReady = true
	return false
}

func (s *State) SetSentReadyOnSuccess(fun func() bool) {
	s.Lock()
	defer s.Unlock()
	// Don't run if sentReady is already true
	if s.sentReady {
		return
	}
	s.sentReady = fun()
}

func (s *State) IncrementEchoCount() int {
	s.Lock()
	defer s.Unlock()
	s.echoCount++
	return s.echoCount
}

func (s *State) IncrementReadyCount() int {
	s.Lock()
	defer s.Unlock()
	s.readyCount++
	return s.readyCount
}

func (s *State) SetFinalValue(value []byte) {
	s.Lock()
	defer s.Unlock()
	s.finalValue = value
	s.finished = true
	s.success = true
}

func (s *State) SetFailedIfNotFinished() {
	s.Lock()
	defer s.Unlock()
	if s.finished {
		return
	}
	s.finalValue = nil
	s.finished = true
	s.success = false
}

func (s *State) AddReadyShareIfAbsent(share *reedsolomon.Encoding) bool {
	s.Lock()
	defer s.Unlock()
	if _, ok := s.readyShares[share]; ok {
		return false
	}
	s.readyShares[share] = struct{}{}
	return true
}
