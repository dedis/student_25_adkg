package agreement

import (
	"fmt"
	"sync"
)

type InstanceManager[T any, C any] struct {
	mu           *sync.Mutex
	instances    map[string]*T
	defaultConf  *C
	createFn     func(*C) *T
	updateConfFn func(base *C, id string) *C
}

func NewInstanceManager[T any, C any](
	defaultConf *C,
	createFn func(*C) *T,
	updateConfFn func(*C, string) *C,
) *InstanceManager[T, C] {
	return &InstanceManager[T, C]{
		// Mutex:        &sync.Mutex{},
		mu:           &sync.Mutex{},
		instances:    make(map[string]*T),
		defaultConf:  defaultConf,
		createFn:     createFn,
		updateConfFn: updateConfFn,
	}
}

func (m *InstanceManager[T, C]) GetOrCreate(id string) *T {
	m.mu.Lock()
	defer m.mu.Unlock()

	if instance, ok := m.instances[id]; ok {
		return instance
	}
	conf := m.updateConfFn(m.defaultConf, id)
	instance := m.createFn(conf)
	m.instances[id] = instance
	return instance
}

func (m *InstanceManager[T, C]) GetOrFail(id string) (*T, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if instance, ok := m.instances[id]; ok {
		return instance, nil
	}
	return nil, fmt.Errorf("instance with id '%s' not found", id)
}

func (m *InstanceManager[T, C]) UpdateDefaultConfig(newConf *C) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultConf = newConf
}

// example with bv_broadcast

// type BVConfig struct {
// 	RoundID    string
// 	NodeID     int
// 	LogEnabled bool
// }

// defaultConf := BVConfig{
// 	NodeID:     42,
// 	LogEnabled: true,
// }

// manager := NewManager[*BVHandler](defaultConf,
// 	func(conf BVConfig) *BVHandler {
// 		return NewBVHandler(conf)
// 	},
// 	func(base BVConfig, id string) BVConfig {
// 		base.RoundID = id
// 		return base
// 	},
// )
