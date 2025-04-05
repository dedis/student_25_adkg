package agreement

import "sync"

// BinSet is a thread-safe data structure consisting of a lock and a boolean array of size 2.
type BinSet struct {
	lock  sync.Mutex
	array [2]bool
}

// NewBinSet creates and returns a new instance of BinSet.
func NewBinSet() *BinSet {
	return &BinSet{
		lock:  sync.Mutex{},
		array: [2]bool{},
	}
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

// ContainsZero checks if the value 0 is in the array.
func (b *BinSet) ContainsZero() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.array[0]
}

// AddValue adds a value (0 or 1) to the array.
func (b *BinSet) AddValue(value int) {
	if value != 0 && value != 1 {
		return // Only 0 or 1 are valid values
	}
	b.lock.Lock()
	defer b.lock.Unlock()
	b.array[value] = true
}

// Values returns a slice of integers representing the values (0 or 1) that are true in the array.
func (b *BinSet) Values() []int {
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
