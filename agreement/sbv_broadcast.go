package agreement

type SBVBroadcast struct {
	nParticipants int
	threshold     int
	// binValues     map[int]int              //pid => binVal // could just be a set?
	// received      map[int]map[int]struct{} //binval => pids from which received
	nodeID    int
	bv        BVBroadcast
	broadcast func(IMessage)
	auxCh     chan int
	// received  map[int]map[int]struct{} //pids from which AUX received => binval
	received map[int]int // I think there is only 1 value from each node
}

func (s *SBVBroadcast) viewPredicate() (bool, [2]bool) {
	// if received set size = n-t take all values it has (0 and 1)
	if len(s.received) > (s.nParticipants - s.threshold) {
		nOnes := 0
		nZeros := 0
		for _, binval := range s.received {
			if binval == 0 {
				nZeros++
			} else if binval == 1 {
				nOnes++
			}
		}
		if nOnes+nZeros < s.nParticipants-s.threshold {
			// got some garbage values, not all of them belong to binValues
			return false, [2]bool{}
		}

		view := [2]bool{false, false}
		predTrue := false
		if nZeros > 0 && ContainsZero(s.bv.BinValues) {
			view[0] = true
			predTrue = true
		}
		if nOnes > 0 && ContainsOne(s.bv.BinValues) {
			view[1] = true
			predTrue = true
		}

		return predTrue, view
	}
	// and if all n-t of them are in binvalues then true (n-x 0 and n-t+x 1 then 1 and 0)
	// all n-t 1 => 1

	// if received > n-t then look for subset such that every val is in binvalues

	return false, [2]bool{}
}

func (s *SBVBroadcast) Broadcast(binValue int) error {
	bvMsg := BVMessage{s.nodeID, binValue}
	notifyCh, err := s.bv.Broadcast(bvMsg, true)
	if err != nil {
		return err
	}

	if len(s.bv.BinValues) == 0 { // TODO Threadsafe
		<-notifyCh
	}

	// // take w from binvalues and aux it
	randomBinVal, _ := randBinSetVal(s.bv.BinValues) // TODO Threadsafe

	msg := AUXMessage{binValue: randomBinVal} //TODO finish

	s.broadcast(&msg)

	// var auxVal int
	// // loop
	// auxVal = <-s.auxCh
	// get notified by handle message if aux
	// call viewPredicate

	return nil
}

// func (s *SBVBroadcast) HandleMessage(m *Message) error {
func (s *SBVBroadcast) HandleMessage(m *AUXMessage) error { // its easiear with concrete msg type
	// dispatch on AUX or B_VAL
	// no dispatch, BV is independent from SBV

	// if aux -> notify

	<-s.auxCh

	return nil
}
