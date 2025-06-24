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
	finished        bool
	success         bool
	value           []byte
	hash            []byte
	predicatePassed bool
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

func (i *Instance) PredicatePassed() bool {
	i.RLock()
	defer i.RUnlock()
	return i.predicatePassed
}

func (i *Instance) SetPredicatePassed() {
	i.Lock()
	defer i.Unlock()
	i.predicatePassed = true
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

func (m *MockRBC) SetPredicate(predicate rbc.Predicate) {
	m.Lock()
	defer m.Unlock()
	m.predicate = predicate
}

func (m *MockRBC) getOrCreateInstance(hash []byte) *Instance {
	state, ok := m.states[string(hash)]
	if ok {
		return state
	}
	state = NewInstance(hash)
	m.states[string(hash)] = state
	return state
}

func (m *MockRBC) GetInstances() []rbc.Instance[[]byte] {
	m.RLock()
	defer m.RUnlock()
	instances := make([]rbc.Instance[[]byte], 0)
	for _, state := range m.states {
		instances = append(instances, state)
	}
	return instances
}

func (m *MockRBC) RBroadcast(msg []byte) (rbc.Instance[[]byte], error) {
	m.Lock()
	defer m.Unlock()
	hasher := sha256.New()
	hasher.Write(msg)
	hash := hasher.Sum(nil)
	state := m.getOrCreateInstance(hash)

	return state, m.iface.Broadcast(msg)
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

		state := m.getOrCreateInstance(hash)

		_ = state.Finish(msg)
		m.finishedChan <- state
		m.Unlock()
	}
}

func (m *MockRBC) GetFinishedChannel() <-chan rbc.Instance[[]byte] {
	return m.finishedChan
}
