package agreement

import (
	"fmt"
	"math/rand"
	"student_25_adkg/agreement/typedefs"
	"sync"
)

// BinSet is a thread-safe data structure consisting of a lock and a boolean array of size 3.
// It represents a set the can contain at most 2 values at a time out of 3 values: 0, 1, undecided.
// if index 0 is true then 0 is in the set
// 1 is true -> 1 is int the set
// 2 is true -> the undecided value is in the set
// only 2 indices can be true at the same time
type BinSet struct {
	lock  sync.Mutex
	array [3]bool
}

// NewBinSet creates and returns a new instance of BinSet.
func NewBinSet() *BinSet {
	return &BinSet{
		lock:  sync.Mutex{},
		array: [3]bool{},
	}
}

// NewBinSetFromArray returns a new instance of BinSet from a given array of 2 booleans.
func (b *BinSet) FromBools(arr [3]bool) (*BinSet, error) {
	count := 0
	for _, v := range b.array {
		if v {
			count++
		}
	}

	if count > 2 {
		return nil, fmt.Errorf("BinSet can contain only 2 values")
	}

	return &BinSet{
		lock:  sync.Mutex{},
		array: arr,
	}, nil
}

// Length returns the number of true values in the array.
func (b *BinSet) Length() int {
	b.lock.Lock()
	defer b.lock.Unlock()
	count := 0
	for _, v := range b.array {
		if v {
			count++
		}
	}
	return count
}

// ContainsOne checks if the value 1 is in the array.
func (b *BinSet) ContainsOne() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.array[1]
}

func (b *BinSet) ContainsUndecided() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.array[2]
}

// ContainsZero checks if the value 0 is in the array.
func (b *BinSet) ContainsZero() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.array[0]
}

// AddValue adds a value (0 or 1) to the array.
func (b *BinSet) AddValue(value int) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.array[value] = true
}

// Values returns a slice of integers representing the values (0 or 1) that are true in the array.
func (b *BinSet) AsInts() []int {
	b.lock.Lock()
	defer b.lock.Unlock()
	values := []int{}
	for i, v := range b.array {
		if v {
			values = append(values, i)
		}
	}
	return values
}

func (b *BinSet) AsBools() [3]bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.array
}

// GetRandomValue returns a random value from the BinSet (0 or 1) if present. If the BinSet is empty, it returns -1.
func (b *BinSet) RandomValue() (int, error) {
	b.lock.Lock()
	defer b.lock.Unlock()

	values := []int{}
	for i, v := range b.array {
		if v {
			values = append(values, i)
		}
	}

	if len(values) == 0 {
		return -1, fmt.Errorf("can't return random value from an empty set")
	}

	return values[rand.Intn(len(values))], nil
}

// ConvertToView converts a BinSet represented as [3]bool to a View type.
func ConvertToView(binSet [3]bool) typedefs.View {
	switch {
	case binSet[0] && binSet[1]:
		return typedefs.View_ZERO_ONE
	case binSet[0] && binSet[2]:
		return typedefs.View_UNDECIDED_ZERO
	case binSet[1] && binSet[2]:
		return typedefs.View_UNDECIDED_ONE
	case binSet[0]:
		return typedefs.View_ZERO
	case binSet[1]:
		return typedefs.View_ONE
	case binSet[2]:
		return typedefs.View_UNDECIDED
	default:
		return typedefs.View_VIEW_UNSPECIFIED
	}
}

// ConvertFromView converts a View type to a BinSet represented as [3]bool.
// Returns an error if the View is View_VIEW_UNSPECIFIED.
func ConvertFromView(view typedefs.View) ([3]bool, error) {
	switch view {
	case typedefs.View_ZERO_ONE:
		return [3]bool{true, true, false}, nil
	case typedefs.View_UNDECIDED_ZERO:
		return [3]bool{true, false, true}, nil
	case typedefs.View_UNDECIDED_ONE:
		return [3]bool{false, true, true}, nil
	case typedefs.View_ZERO:
		return [3]bool{true, false, false}, nil
	case typedefs.View_ONE:
		return [3]bool{false, true, false}, nil
	case typedefs.View_UNDECIDED:
		return [3]bool{false, false, true}, nil
	case typedefs.View_VIEW_UNSPECIFIED:
		return [3]bool{}, fmt.Errorf("invalid view: View_VIEW_UNSPECIFIED")
	default:
		return [3]bool{}, fmt.Errorf("invalid view")
	}
}
