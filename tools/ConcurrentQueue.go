package tools

import (
	"errors"
	"sync"
)

// Queue is an interface representing a simple queue that can be popped and pushed
type Queue[T any] interface {
	// Push a new element in the queue. Returns an error if the queue is full
	Push(T) error
	// Pop the oldest element in the queue. Returns an error if the queue is empty
	Pop() (T, error)
	// IsEmpty returns true if the map is empty
	IsEmpty() bool
	// IsFull returns true if the map is full
	IsFull() bool
}

// ConcurrentQueue is an implementation of Queue that is thread safe
type ConcurrentQueue[T any] struct {
	queue    []T
	capacity int
	head     int
	tail     int
	empty    bool
	sync.RWMutex
}

// NewConcurrentQueue returns a new empty ConcurrentQueue with the given max capacity
func NewConcurrentQueue[T any](capacity int) *ConcurrentQueue[T] {
	return &ConcurrentQueue[T]{
		queue:    make([]T, capacity),
		capacity: capacity,
		head:     0,
		tail:     0,
		empty:    true,
		RWMutex:  sync.RWMutex{},
	}
}

// Push implements the Push method from Queue
func (q *ConcurrentQueue[T]) Push(el T) error {
	q.Lock()
	defer q.Unlock()
	// Check if the queue is full
	if q.IsFull() {
		return errors.New("queue is full")
	}

	// Push the new element at the current tail
	q.queue[q.tail] = el
	// Update the tail pointer
	q.tail = (q.tail + 1) % q.capacity
	q.empty = false
	return nil
}

// Pop implements the Pop method from Queue
func (q *ConcurrentQueue[T]) Pop() (T, error) {
	q.Lock()
	defer q.Unlock()
	// Check if the queue is empty
	if q.IsEmpty() {
		return q.queue[0], errors.New("queue is empty")
	}
	// Return the element at the current head position
	msg := q.queue[q.head]
	// Update the head pointer
	q.head = (q.head + 1) % q.capacity
	if q.head == q.tail {
		q.empty = true
	}
	return msg, nil
}

// IsEmpty implements the IsFull method from Queue
func (q *ConcurrentQueue[T]) IsEmpty() bool {
	return q.empty
}

// IsFull implements the IsEmpty method from Queue
func (q *ConcurrentQueue[T]) IsFull() bool {
	return q.tail == q.head && !q.empty
}
