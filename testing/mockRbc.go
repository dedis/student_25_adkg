package testing

import (
	"context"
	"crypto/sha256"
	"errors"
	"student_25_adkg/logging"
	"student_25_adkg/networking"
	"student_25_adkg/rbc"
	"sync"

	"github.com/rs/zerolog"
)

type Instance struct {
	finished bool
	success  bool
	value    []byte
	hash     []byte
	sync.RWMutex
}

func NewInstance(messageHash []byte) *Instance {
	return &Instance{
		hash: messageHash,
	}
}

func (i *Instance) Identifier() []byte {
	i.RLock()
	defer i.RUnlock()
	return i.hash
}

func (i *Instance) GetValue() []byte {
	i.RLock()
	defer i.RUnlock()
	return i.value
}

func (i *Instance) Finished() bool {
	i.RLock()
	defer i.RUnlock()
	return i.finished
}

func (i *Instance) Success() bool {
	i.RLock()
	defer i.RUnlock()
	return i.success
}

func (i *Instance) Finish(value []byte) bool {
	i.Lock()
	defer i.Unlock()
	if i.finished {
		return false
	}
	i.finished = true
	i.value = value
	i.success = true
	return true
}

type MockRBC struct {
	predicate    func([]byte) bool
	finishedChan chan rbc.Instance[[]byte]
	iface        networking.NetworkInterface
	states       map[string]*Instance
	logger       zerolog.Logger
	sync.RWMutex
}

func NewMockRBC(iface networking.NetworkInterface, predicate func([]byte) bool) *MockRBC {
	return &MockRBC{
		predicate:    predicate,
		finishedChan: make(chan rbc.Instance[[]byte]),
		iface:        iface,
		states:       make(map[string]*Instance),
		logger:       logging.GetLogger(iface.GetID()),
	}
}

func (m *MockRBC) SetPredicate(predicate func([]byte) bool) {
	m.Lock()
	defer m.Unlock()
	m.predicate = predicate
}

func (m *MockRBC) RBroadcast(msg []byte) error {
	return m.iface.Broadcast(msg)
}

func (m *MockRBC) Listen(ctx context.Context) error {
	for {
		msg, err := m.iface.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			continue
		}

		if !m.predicate(msg) {
			m.logger.Error().Err(err).Msg("message did not pass predicate")
			return rbc.ErrPredicateRejected
		}

		m.logger.Info().Msg("Finished instance")

		m.Lock()
		hasher := sha256.New()
		hasher.Write(msg)
		hash := hasher.Sum(nil)

		state, ok := m.states[string(hash)]
		if !ok {
			state = NewInstance(hash)
			m.states[string(hash)] = state
		}

		_ = state.Finish(msg)
		m.finishedChan <- state
		m.Unlock()
	}
}

func (m *MockRBC) GetFinishedChannel() <-chan rbc.Instance[[]byte] {
	return m.finishedChan
}
