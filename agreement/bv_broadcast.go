package agreement

import (
	"fmt"
	"sync"
)

type BVBroadcast struct {
	*sync.RWMutex
	nParticipants int
	threshold     int
	// BinValues     map[int]struct{}         // TODO Threadsafe
	// BinValues    map[int]struct{}         // set of at most 2 (0 and 1) TODO Threadsafe
	BinValues    BinSet                   // set of at most 2 (0 and 1) TODO Threadsafe
	received     map[int]map[int]struct{} //binval => pids from which received
	notifyCh     chan struct{}            // notify caller about new binvalue (can be nil?)
	shouldNotify bool
	broadcasted  [2]bool // false is not yet broadcasted TODO Threadsafe
	broadcast    func(IMessage)
	nodeID       int

	// received[msg.binValue] initialize for 0 and 1 in the beginning
	// received      map[int]struct{} // pid => binval
}

// NewBVBroadcast creates and returns a new instance of BVBroadcast.
func NewBVBroadcast(nParticipants, threshold, nodeID int, broadcast func(IMessage)) *BVBroadcast {
	return &BVBroadcast{
		RWMutex:       &sync.RWMutex{},
		nParticipants: nParticipants,
		threshold:     threshold,
		BinValues:     *NewBinSet(),
		received:      make(map[int]map[int]struct{}),
		notifyCh:      make(chan struct{}),
		broadcasted:   [2]bool{false, false},
		broadcast:     broadcast,
		nodeID:        nodeID,
	}
}

// 1: bin_values ← ∅
// 2: send BVAL(v) to all
// 3: return bin_values . bin_values has not necessarily
// reached its final value when returned
// 4: upon receiving BVAL(v) do
// 5: 	if BVAL(v) received from t + 1 different nodes then
// 6: 		send BVAL(v) to all (if haven’t done already)
// 7: 	if BVAL(v) received from 2t + 1 different nodes then
// 8: 		bin_values ← bin_values ∪ {v}

func (b *BVBroadcast) Broadcast(msg BVMessage, trackUpdates bool) (chan struct{}, error) {
	// broadcast
	// how do I return binvalues here? they are empty...
	// should I return a channel to notify about new incoming binvalues?
	b.shouldNotify = trackUpdates
	return b.notifyCh, nil
}

// should keep track of rounds?
// TODO pass interface?
func (b *BVBroadcast) HandleMessage(msg BVMessage) (int, bool, error) {

	// this should be received instead of binval
	if msg.binValue != 0 && msg.binValue != 1 {
		return 0, false, fmt.Errorf("invalid binary value %d from node %d", msg.binValue, msg.sourceNode)
	}

	if _, ok := b.received[msg.binValue][msg.sourceNode]; ok {
		return 0, false, fmt.Errorf("redundant bv_broadcast from node %d", msg.sourceNode)
	}

	// b.binValues[msg.sourceNode] = msg.binValue

	if len(b.received[msg.binValue]) >= b.threshold+1 && !b.broadcasted[msg.binValue] { //TODO and v not yet broadcasted
		b.broadcasted[msg.binValue] = true
		b.broadcast(&msg)
		// broadcast binValue asynchronously
		// if haven't done already. Should done why? When?
		// return msg or send to outbox
	}

	if len(b.received[msg.binValue]) == 2*b.threshold+1 { // && hasn't seen but it's a set so does not natter
		// b.BinValues[msg.binValue] = struct{}{}
		// b.BinValues[msg.binValue] = struct{}{}
		b.BinValues.AddValue(msg.binValue)
		echoMsg := &BVMessage{sourceNode: b.nodeID, binValue: msg.binValue}
		b.broadcast(echoMsg)
		b.notifyCh <- struct{}{} // or close it
		// return? like
		// return msg.binValue, true, nil

	}

	return 0, false, nil // TODO change for real return value
}
