package bracha

import "sync"

type State struct {
	echoCount  int
	readyCount int
	sentReady  bool
	finished   bool
	success    bool
	value      bool
	sync.RWMutex
}

func NewState() *State {
	return &State{}
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

func (s *State) Value() bool {
	s.RLock()
	defer s.RUnlock()
	return s.value
}

func (s *State) Success() bool {
	s.RLock()
	defer s.RUnlock()
	return s.success
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
