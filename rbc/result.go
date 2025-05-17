package rbc

import "sync"

// Resulting represents something that produces a result
type Resulting[R any] interface {
	// GetResult returns the result of this object or ErrNotFinished if
	// the result is not yet ready
	GetResult() (R, error)
	// IsFinished returns true if the result if ready. GetResult should
	// return a value if this returns true.
	IsFinished() bool
	// SetResult sets the result value and marks as ready. I.e. IsFinished
	// should return true after this
	SetResult(R)
	// GetFinishedChan returns a channel that will be closed when SetResult
	// is called
	GetFinishedChan() <-chan struct{}
}

// Result is a result of some protocol
type Result[R any] struct {
	result       R
	isFinished   bool
	finishedChan chan struct{}
	sync.RWMutex
}

func NewResult[R any]() *Result[R] {
	return &Result[R]{
		isFinished:   false,
		finishedChan: make(chan struct{}),
		RWMutex:      sync.RWMutex{},
	}
}

// GetResult implements Resulting
func (r *Result[R]) GetResult() (R, error) {
	r.RLock()
	defer r.RUnlock()
	if !r.isFinished {
		return r.result, ErrNotFinished
	}
	return r.result, nil
}

// IsFinished implements Resulting
func (r *Result[R]) IsFinished() bool {
	r.RLock()
	defer r.RUnlock()
	return r.isFinished
}

// SetResult implements Resulting
func (r *Result[R]) SetResult(result R) {
	r.Lock()
	defer r.Unlock()
	r.result = result
	r.isFinished = true
	close(r.finishedChan)
}

// GetFinishedChan implements Resulting
func (r *Result[R]) GetFinishedChan() <-chan struct{} {
	return r.finishedChan
}
