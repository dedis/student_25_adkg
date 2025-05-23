package fourrounds

import (
	"student_25_adkg/reedsolomon"
	"sync"
)

type State struct {
	sentReady        bool
	echoCount        int
	readyCount       int
	readyShares      map[*reedsolomon.Encoding]struct{}
	finalValue       []byte
	finished         bool
	success          bool
	finishedChannels []chan struct{}
	messageHash      []byte
	sync.RWMutex
}

func NewState(messageHash []byte) *State {
	return &State{
		sentReady:        false,
		echoCount:        0,
		readyCount:       0,
		readyShares:      make(map[*reedsolomon.Encoding]struct{}),
		finalValue:       nil,
		finished:         false,
		success:          false,
		finishedChannels: make([]chan struct{}, 0),
		messageHash:      messageHash,
		RWMutex:          sync.RWMutex{},
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

func (s *State) FinalValue() []byte {
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
	s.notifyChannels()
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
	s.notifyChannels()
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

func (s *State) GetFinishedChan() <-chan struct{} {
	s.Lock()
	defer s.Unlock()
	finishedChan := make(chan struct{})
	if s.finished {
		close(finishedChan)
		// Don't store the closed channel
		return finishedChan
	}
	// Return a closed channel if the protocol is already finished
	s.finishedChannels = append(s.finishedChannels, finishedChan)
	return finishedChan
}

func (s *State) notifyChannels() {
	// Don't need to lock as it is a private method that will only be called by methods already locked
	for _, ch := range s.finishedChannels {
		close(ch)
	}
	s.finishedChannels = make([]chan struct{}, 0)
}
