package agreement

import (
	"fmt"
	"sync"
)

type BVBroadcast struct { // TODO: add logger
	*sync.RWMutex
	nParticipants int
	threshold     int
	BinValues     BinSet                   // set of at most 2 (0 and 1) TODO Threadsafe
	received      map[int]map[int]struct{} //binval => pids from which received
	notifyCh      chan struct{}            // notify caller about new binvalue (can be nil?)
	shouldNotify  bool
	broadcasted   [2]bool // false is not yet broadcasted TODO Threadsafe
	broadcast     func(IMessage) error
	nodeID        int
}

func NewBVBroadcast(nParticipants, threshold, nodeID int, broadcast func(IMessage) error) *BVBroadcast {
	received := make(map[int]map[int]struct{})
	received[0] = make(map[int]struct{})
	received[1] = make(map[int]struct{})

	return &BVBroadcast{
		RWMutex:       &sync.RWMutex{},
		nParticipants: nParticipants,
		threshold:     threshold,
		BinValues:     *NewBinSet(),
		received:      received,
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

func (b *BVBroadcast) Broadcast(msg *BVMessage, trackUpdates bool) (chan struct{}, error) {
	b.shouldNotify = trackUpdates
	if !b.broadcasted[msg.binValue] {
		b.broadcasted[msg.binValue] = true
		err := b.broadcast(msg)

		if err != nil {
			return nil, fmt.Errorf("error broadcasting message %v %w", msg, err)
		}
	}

	return b.notifyCh, nil
}

// should keep track of rounds?
// TODO pass interface?
func (b *BVBroadcast) HandleMessage(msg *BVMessage) (int, bool, error) {

	if msg.binValue != 0 && msg.binValue != 1 {
		return 0, false, fmt.Errorf("invalid binary value %d from node %d", msg.binValue, msg.sourceNode)
	}

	if _, ok := b.received[msg.binValue][msg.sourceNode]; ok {
		return 0, false, fmt.Errorf("redundant bv_broadcast from node %d", msg.sourceNode)
	}

	b.received[msg.binValue][msg.sourceNode] = struct{}{}
	// fmt.Printf("at node %d: BVMessage %d %d, received: %v\n", b.nodeID, msg.sourceNode, msg.binValue, b.received[1])

	if len(b.received[msg.binValue]) >= b.threshold+1 && !b.broadcasted[msg.binValue] { //lock broadcasted
		// fmt.Printf("node %d, broadcasted: %v\n", b.nodeID, b.broadcasted[msg.binValue])
		b.broadcasted[msg.binValue] = true
		echoMsg := &BVMessage{sourceNode: b.nodeID, binValue: msg.binValue}
		err := b.broadcast(echoMsg)
		if err != nil {
			return 0, false, fmt.Errorf("error broadcasting message %v %w", msg, err)
		}
	}

	if len(b.received[msg.binValue]) == 2*b.threshold+1 { // lock received?
		b.BinValues.AddValue(msg.binValue)
		if b.shouldNotify {
			b.notifyCh <- struct{}{} // or close it
		}
		return msg.binValue, true, nil

	}

	return 0, false, nil // TODO change for real return value
}
