package rbc

import (
	"errors"
	"sync"
)

var ErrNoInstance = errors.New("no instance found")

// Handler handles multiple instances of RBC broadcasting values of type T
type Handler[T any] struct {
	instances map[InstanceIdentifier]Instance[T]
	sync.RWMutex
}

func NewHandler[T any, M interface{}]() *Handler[T] {
	return &Handler[T]{
		instances: make(map[InstanceIdentifier]Instance[T]),
	}
}

// RegisterIfAbsent registers the given instance if an instance with
// the same identifier does not already exist. Returns true if the
// instance was registered, false otherwise
func (h *Handler[T]) RegisterIfAbsent(instance Instance[T]) bool {
	h.Lock()
	defer h.Unlock()
	_, ok := h.instances[instance.GetIdentifier()]
	if !ok {
		h.instances[instance.GetIdentifier()] = instance
	}
	return !ok
}

// GetInstance returns the instance linked to the given instance or ErrNoInstance if
// there is no such instance
func (h *Handler[T]) GetInstance(identifier InstanceIdentifier) (Instance[T], error) {
	h.RLock()
	defer h.RUnlock()
	instance, ok := h.instances[identifier]
	if !ok {
		return instance, ErrNoInstance
	}
	return instance, nil
}

// HandleMessage handles a message destined to a given instance. Returns the result
// of Instance.HandleMessage
func (h *Handler[T]) HandleMessage(msg Message) (Message, error) {
	h.RLock()
	defer h.RUnlock()
	instance, ok := h.instances[msg.GetIdentifier()]
	msg.GetIdentifier()
	if !ok {
		return nil, ErrNoInstance
	}
	return instance.HandleMessage(msg), nil
}
