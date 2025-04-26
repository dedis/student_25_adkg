package agreement

type BinValsReceiver interface {
	Lock()
	Unlock()
	Received() map[int]int // binvalue -> sender
}

func AuxSetViewPredicateOneVal(received map[int][]int, n int, t int, binVal int) (bool, [3]bool) {
	nVals := len(received[binVal])
	if nVals >= (n - t) {
		view := [3]bool{false, false}
		predTrue := false

		view[binVal] = true
		predTrue = true

		return predTrue, view
	}
	return false, [3]bool{}
}
func AuxSetViewPredicateTwoVals(received map[int][]int, n int, t int, binVal1, binVal2 int) (bool, [3]bool) {
	nVals1 := len(received[binVal1])
	nVals2 := len(received[binVal2])

	if nVals1+nVals2 >= (n - t) {
		view := [3]bool{false, false}
		predTrue := false
		if nVals1 > 0 {
			view[binVal1] = true
			predTrue = true
		}
		if nVals2 > 0 {
			view[binVal2] = true
			predTrue = true
		}

		return predTrue, view
	}
	return false, [3]bool{}
}

// received: binval -> pids
func AuxSetViewPredicate(received map[int][]int, n int, t int, binVals *BinSet) (bool, [3]bool) {

	validBinVals := binVals.AsInts()
	if len(validBinVals) == 1 {
		return AuxSetViewPredicateOneVal(received, n, t, validBinVals[0])
	} else if len(validBinVals) == 2 {
		return AuxSetViewPredicateTwoVals(received, n, t, validBinVals[0], validBinVals[1])
	}
	return false, [3]bool{}
}

func AuxViewPredicateOneVal(s BinValsReceiver, n int, t int, binVal int) (bool, [3]bool) {
	if len(s.Received()) >= (n - t) {
		nVals := 0
		for _, val := range s.Received() {
			if val == binVal {
				nVals++
			}
		}
		if nVals < n-t {
			// some garbage values among received
			return false, [3]bool{}
		}
		view := [3]bool{false, false}
		predTrue := false

		view[binVal] = true
		predTrue = true

		return predTrue, view
	}
	return false, [3]bool{}
}
func AuxViewPredicateTwoVals(s BinValsReceiver, n int, t int, binVal1, binVal2 int) (bool, [3]bool) {
	if len(s.Received()) >= (n - t) {
		nVals1 := 0
		nVals2 := 0
		for _, binval := range s.Received() {
			if binval == binVal1 {
				nVals1++
			} else if binval == binVal2 {
				nVals2++
			}
		}
		if nVals1+nVals2 < n-t {
			// some garbage values among received
			return false, [3]bool{}
		}

		view := [3]bool{false, false}
		predTrue := false
		if nVals1 > 0 {
			view[binVal1] = true
			predTrue = true
		}
		if nVals2 > 0 {
			view[binVal2] = true
			predTrue = true
		}

		return predTrue, view
	}
	return false, [3]bool{}
}

// n: number of nodes
// t: tolerable threshold of byzantine nodes
// If #received >= n-t then look for subset of (n-t) values
// such that every value from it is also in binvalues.
// If such subset exists and
// 1) contains only ones => view := {1}
// 2) contains only zeros => view := {0}
// 3) contains (n-x) zeros and (n-t+x) ones => view := {0, 1}
func AuxViewPredicate(s BinValsReceiver, n int, t int, binVals *BinSet) (bool, [3]bool) {
	s.Lock()
	defer s.Unlock()
	validBinVals := binVals.AsInts()
	if len(validBinVals) == 1 {
		return AuxViewPredicateOneVal(s, n, t, validBinVals[0])
	} else if len(validBinVals) == 2 {
		return AuxViewPredicateTwoVals(s, n, t, validBinVals[0], validBinVals[1])
	}
	return false, [3]bool{}
}
