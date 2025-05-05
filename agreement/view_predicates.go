package agreement

// Implementation of AuxViewPredicate and AuxSetViewPredicate.
// These predicate functions are quite similar but I decided not to unite
// them into one single predicate. The "received" map passed as an argument
// to AuxSetViewPredicate has format (binvalue -> slice of senders) while
// BinValsReceiver.Received() used in BinValsReceiver is (sender -> binvalue).
// Converting them to the same format each time before calling the predicate means
// unnecessary computation and using these different formats in other parts of
// the code is more convenient.

// 1. AuxViewPredicate
// When the predicate is true, sbv broadcast is terminated.

// n: number of nodes
// t: tolerable threshold of byzantine nodes
// If #(senders of aux msgs) >= n-t then look for subset of (n-t) values
// such that every value from it is also in binvalues.
// If such subset exists and
// 1) contains only ones => view := {1}
// 2) contains only zeros => view := {0}
// 3) contains (n-x) zeros and (n-t+x) ones => view := {0, 1}

// Note: binvalues can contain not only 0 or 1, but also an "undecided" value (encoded as 2).
// That makes view := any of {2}, {0, 2}, {1, 2} valid as well
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

// 2. AuxSetViewPredicate
// When the predicate is true, ABA round continues from stage 0 to 1.
// received: binval -> pids

// Same setting as in AuxViewPredicate but with auxset msgs.
func AuxSetViewPredicate(received map[int][]int, n int, t int, binVals *BinSet) (bool, [3]bool) {

	validBinVals := binVals.AsInts()
	if len(validBinVals) == 1 {
		return AuxSetViewPredicateOneVal(received, n, t, validBinVals[0])
	} else if len(validBinVals) == 2 {
		return AuxSetViewPredicateTwoVals(received, n, t, validBinVals[0], validBinVals[1])
	}
	return false, [3]bool{}
}

type BinValsReceiver interface {
	Lock()
	Unlock()
	Received() map[int]int // sender -> binvalue
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
		view := [3]bool{}
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

		view := [3]bool{}
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
