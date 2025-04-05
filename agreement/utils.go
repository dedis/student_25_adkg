package agreement

import (
	"fmt"
	"math/rand"
)

func randBinSetVal(binvals map[int]struct{}) (int, error) {
	var first, second int
	i := 0

	// Just loop once or twice
	for key := range binvals {
		if i == 0 {
			first = key
		} else {
			second = key
		}
		i++
	}

	switch i {
	case 1:
		return first, nil
	case 2:
		if rand.Intn(2) == 0 {
			return first, nil
		} else {
			return second, nil
		}
	}

	return -1, fmt.Errorf("empty set")
}
