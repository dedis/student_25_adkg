package rbc

import "errors"

var ErrNoInstance = errors.New("no instance found")

// Handler handles multiple instances of RBC
type Handler[T any, M interface{}] struct {
	instances map[InstanceIdentifier]Instance[T, M]
}

func NewHandler[T any, M interface{}]() *Handler[T, M] {
	return &Handler[T, M]{
		instances: make(map[InstanceIdentifier]Instance[T, M]),
	}
}

// RegisterIfAbsent registers the given instance if an instance with
// the same identifier does not already exist. Returns true if the
// instance was registered, false otherwise
func (h *Handler[T, M]) RegisterIfAbsent(instance Instance[T, M]) bool {
	_, ok := h.instances[instance.GetIdentifier()]
	if !ok {
		h.instances[instance.GetIdentifier()] = instance
	}
	return !ok
}

// Get returns the instance linked to the given instance or ErrNoInstance if
// there is no such instance
func (h *Handler[T, M]) Get(identifier InstanceIdentifier) (Instance[T, M], error) {
	instance, ok := h.instances[identifier]
	if !ok {
		return instance, ErrNoInstance
	}
	return instance, nil
}

// HandleMessage handles a message destined to a given instance. Returns the result
// of Instance.HandleMessage
func (h *Handler[T, M]) HandleMessage(identifier InstanceIdentifier, msg *M) (*M, error) {
	instance, ok := h.instances[identifier]
	if !ok {
		return nil, ErrNoInstance
	}
	return instance.HandleMessage(msg), nil
}
