package bracha

import (
	"encoding/binary"
	"sync"
)

type State struct {
	instanceID      uint32
	echoCount       int
	readyCount      int
	sentReady       bool
	finished        bool
	success         bool
	value           bool
	predicatePassed bool
	sync.RWMutex
}

func NewState(instanceID uint32) *State {
	return &State{instanceID: instanceID}
}

func (s *State) EchoCount() int {
	s.RLock()
	defer s.RUnlock()
	return s.echoCount
}

func (s *State) SentReady() bool {
	s.RLock()
	defer s.RUnlock()
	return s.sentReady
}

func (s *State) Identifier() []byte {
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, s.instanceID)
	return bs
}

func (s *State) GetValue() bool {
	s.RLock()
	defer s.RUnlock()
	return s.value
}

func (s *State) Success() bool {
	s.RLock()
	defer s.RUnlock()
	return s.success
}

func (s *State) PredicatePassed() bool {
	s.RLock()
	defer s.RUnlock()
	return s.predicatePassed
}

func (s *State) SetPredicatePassed() {
	s.Lock()
	defer s.Unlock()
	s.predicatePassed = true
}

func (s *State) FailIfNotFinished() bool {
	s.Lock()
	defer s.Unlock()
	if s.finished {
		return false
	}
	s.success = false
	s.finished = true
	return true
}

func (s *State) SetFinalValue(value bool) {
	s.Lock()
	defer s.Unlock()
	s.value = value
	s.success = true
	s.finished = true
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

func (s *State) SetSentReady() {
	s.Lock()
	defer s.Unlock()
	s.sentReady = true
}

func (s *State) Finished() bool {
	s.RLock()
	defer s.RUnlock()
	return s.finished
}

func (s *State) FinalValue() bool {
	s.RLock()
	defer s.RUnlock()
	return s.value
}
