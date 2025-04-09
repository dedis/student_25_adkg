package agreement

import (
	"fmt"
	"sync"
)

// Figure 1 from Signature-Free_Asynchronous_Binary_Byzantine_Consensus
// 1: bin_values ← ∅
// 2: send BVAL(v) to all
// 3: return bin_values . bin_values has not necessarily
// reached its final value when returned
// 4: upon receiving BVAL(v) do
// 5: 	if BVAL(v) received from t + 1 different nodes then
// 6: 		send BVAL(v) to all (if haven’t done already)
// 7: 	if BVAL(v) received from 2t + 1 different nodes then
// 8: 		bin_values ← bin_values ∪ {v}

// TODO: add logger
type BVBroadcast struct {
	*sync.RWMutex // to lock received and broadcasted
	nParticipants int
	threshold     int
	BinValues     BinSet                   // Threadsafe set of at most 2 values (0 and 1)
	received      map[int]map[int]struct{} // binval => pids from which received
	broadcasted   [2]bool                  // false means that idx is not yet broadcasted as binVal
	notifyCh      chan struct{}            // notify caller about new binvalue
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
		notifyCh:      make(chan struct{}, 2), //can have 0 and 1
		broadcasted:   [2]bool{false, false},
		broadcast:     broadcast,
		nodeID:        nodeID,
	}
}

// BV is supposed to return set binValues to SBV and SBV is blocked
// until binValues is not empty.
// Instead returns a channel to notify SBV about updates to binValues.
func (b *BVBroadcast) Broadcast(msg *BVMessage) (chan struct{}, error) {
	b.Lock()
	// could have already echoed this binValue before broadcasted its own
	shouldBroadcast := !b.broadcasted[msg.binValue]
	if shouldBroadcast {
		b.broadcasted[msg.binValue] = true
	}
	b.Unlock()

	if shouldBroadcast {
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

	b.Lock()
	if _, ok := b.received[msg.binValue][msg.sourceNode]; ok {
		b.Unlock()
		return 0, false, fmt.Errorf("redundant bv_broadcast from node %d", msg.sourceNode)
	}
	b.received[msg.binValue][msg.sourceNode] = struct{}{}
	// fmt.Printf("num received at %d is %d\n", b.nodeID, b.received)

	shouldBroadcast := false
	if len(b.received[msg.binValue]) >= b.threshold+1 && !b.broadcasted[msg.binValue] {
		b.broadcasted[msg.binValue] = true
		shouldBroadcast = true
	}
	b.Unlock()

	if shouldBroadcast {
		echoMsg := &BVMessage{sourceNode: b.nodeID, binValue: msg.binValue}
		err := b.broadcast(echoMsg)
		if err != nil {
			return 0, false, fmt.Errorf("error broadcasting message %v %w", msg, err)
		}
	}
	b.RLock()
	defer b.RUnlock()

	if len(b.received[msg.binValue]) >= 2*b.threshold+1 && !b.BinValues.AsBools()[msg.binValue] {
		b.BinValues.AddValue(msg.binValue)

		b.notifyCh <- struct{}{}
		return msg.binValue, true, nil
	}

	return 0, false, nil // TODO maybe return value not needed at all
}
