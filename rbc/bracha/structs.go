package bracha

import "sync"

type SafeCounter struct {
	count int
	mu    sync.RWMutex
}

func NewCounter() *SafeCounter {
	return &SafeCounter{count: 0}
}

func (s *SafeCounter) Inc() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	return s.count
}

func (s *SafeCounter) Value() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

type State struct {
	echoCount  *SafeCounter
	readyCount *SafeCounter
	readySent  bool
	sync.RWMutex
}

func NewState() *State {
	return &State{
		echoCount:  NewCounter(),
		readyCount: NewCounter(),
		readySent:  false,
		RWMutex:    sync.RWMutex{},
	}
}

func (s *State) IncrementEchoCount() int {
	return s.echoCount.Inc()
}

func (s *State) IncrementReadyCount() int {
	return s.readyCount.Inc()
}

func (s *State) SetSentReady(set bool) {
	s.Lock()
	defer s.Unlock()
	s.readySent = set
}

func (s *State) GetEchoCount() int {
	return s.echoCount.Value()
}

func (s *State) GetReadyCount() int {
	return s.readyCount.Value()
}

func (s *State) GetReadySent() bool {
	s.RLock()
	defer s.RUnlock()
	return s.readySent
}
