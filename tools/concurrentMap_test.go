package tools

import (
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

// Simple test to ensure an single element is added correctly
func TestConcurrentMap_GetSet(t *testing.T) {
	m := NewConcurrentMap[int, int]()
	n := 0
	for i := 0; i < 10; i++ {
		m.Set(i, n)
	}

	require.True(t, len(m.m) == 10)

	for i := 0; i < 10; i++ {
		ni, ok := m.Get(i)
		require.True(t, ok)
		require.Equal(t, ni, n)
	}
}

func TestConcurrentMap_GetOrSet_LotOfNodes(t *testing.T) {
	nbNodes := 10
	m := NewConcurrentMap[int, int]()

	wg := sync.WaitGroup{}

	// Each node i will concurently add 5 entries (i*10+j) mapped to i
	// I.e. no overlapp between the values so we expect no problem
	perNode := 5
	for i := 0; i < nbNodes; i++ {
		wg.Add(1)
		go func() {
			base := i * 10
			for j := 0; j < perNode; j++ {
				m.Set(base+j, i)
			}
			wg.Done()
		}()
	}

	wg.Wait()

	// Check that all values have been added
	require.Equal(t, nbNodes*perNode, len(m.m))

}

func TestConcurrentMap_GetSet_LotOfNodesCounting(t *testing.T) {
	// 100 nodes will concurently modify the same value to increment its count. At the end the value should be 100
	m := NewConcurrentMap[int, int]()
	wg := sync.WaitGroup{}
	target := 1
	nbNodes := 100
	for i := 0; i < nbNodes; i++ {
		wg.Add(1)
		go func() {
			m.DoAndSet(target, func(c int, ok bool) int {
				if !ok {
					c = 0
				}
				return c + 1
			})
			wg.Done()
		}()
	}

	wg.Wait()
	val, ok := m.Get(target)
	require.True(t, ok)
	require.Equal(t, nbNodes, val)
}
