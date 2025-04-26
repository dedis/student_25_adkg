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
