package agreement

import (
	"fmt"
	"math/rand"
)

func uniqueRandomInts(n, _min, _max int) ([]int, error) {
	if _max-_min < n {
		return nil, fmt.Errorf("range too small for %d unique values", n)
	}

	// create slice of all values
	pool := make([]int, _max-_min)
	for i := range pool {
		pool[i] = _min + i
	}

	rand.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})

	return pool[:n], nil
}

// findCombinations generates all combinations of `combSize` indexes from a total of `totalSize`.
func findCombinations(totalSize, combSize int) [][]int {
	indexes := make([]int, totalSize)
	for i := 0; i < totalSize; i++ {
		indexes[i] = i
	}

	var combinations [][]int
	var helper func(start int, current []int)
	helper = func(start int, current []int) {
		if len(current) == combSize {
			combo := make([]int, combSize)
			copy(combo, current)
			combinations = append(combinations, combo)
			return
		}

		for i := start; i < totalSize; i++ {
			helper(i+1, append(current, indexes[i]))
		}
	}

	helper(0, []int{})
	return combinations
}
