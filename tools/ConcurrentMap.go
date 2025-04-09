package tools

import (
	"sync"
)

type ConcurrentMap[K comparable, V any] struct {
	m map[K]V
	sync.RWMutex
}

func NewConcurrentMap[K comparable, V any]() *ConcurrentMap[K, V] {
	return &ConcurrentMap[K, V]{
		m:       make(map[K]V),
		RWMutex: sync.RWMutex{},
	}
}

func (c *ConcurrentMap[K, V]) Get(k K) (V, bool) {
	c.RLock()
	defer c.RUnlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *ConcurrentMap[K, V]) GetOrDefault(k K, def V) V {
	c.RLock()
	defer c.RUnlock()
	v, ok := c.m[k]
	if !ok {
		return def
	}
	return v
}

func (c *ConcurrentMap[K, V]) Set(k K, v V) {
	c.Lock()
	defer c.Unlock()
	c.m[k] = v
}

func (c *ConcurrentMap[K, V]) DoAndSet(k K, do func(V, bool) V) {
	c.Lock()
	defer c.Unlock()
	v, ok := c.m[k]
	newV := do(v, ok)
	c.m[k] = newV
}
