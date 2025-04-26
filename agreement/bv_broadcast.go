package agreement

import (
	"fmt"
	"student_25_adkg/agreement/typedefs"
	"sync"

	"google.golang.org/protobuf/proto"
)

type BVBroadcast struct {
	mu sync.RWMutex // to lock received and broadcasted
	// instanceID    string
	nParticipants int
	threshold     int
	BinValues     BinSet                   // Threadsafe set of at most 2 values (0 and 1)
	received      map[int]map[int]struct{} // binval => pids from which received
	broadcasted   [3]bool                  // false means that idx is not yet broadcasted as binVal
	notifyCh      chan struct{}            // notify caller about new binvalue
	errorCh       chan error               // notify caller about new binvalue
	broadcast     func(proto.Message) error
	nodeID        int
}

type BVBroadcastConfig struct {
	NParticipants int
	Threshold     int
	NodeID        int
	RoundID       string
	BroadcastFn   func(proto.Message) error
}

func NewBVBroadcastFromConfig(conf *BVBroadcastConfig) *BVBroadcast {
	received := make(map[int]map[int]struct{})
	received[0] = make(map[int]struct{})
	received[1] = make(map[int]struct{})
	received[2] = make(map[int]struct{})

	return &BVBroadcast{
		// RWMutex:       &sync.RWMutex{},
		nParticipants: conf.NParticipants,
		threshold:     conf.Threshold,
		BinValues:     *NewBinSet(),
		received:      received,
		notifyCh:      make(chan struct{}, 3), //can have 0 and 1, or 2 as undecided
		broadcasted:   [3]bool{false, false, false},
		broadcast:     conf.BroadcastFn,
		nodeID:        conf.NodeID,
	}
}

// BV is supposed to return set binValues to SBV and SBV is blocked
// until binValues is not empty.
// Instead returns a channel to notify SBV about updates to binValues.
func (b *BVBroadcast) Propose(roundID string, binValue int) (chan struct{}, error) {
	b.mu.Lock()
	msg := &typedefs.BVMessage{SourceNode: int32(b.nodeID), BinValue: int32(binValue), RoundId: roundID}
	// could have already echoed this binValue before broadcasted its own
	shouldBroadcast := !b.broadcasted[binValue]
	if shouldBroadcast {
		b.broadcasted[binValue] = true
	}
	b.mu.Unlock()

	if shouldBroadcast {
		err := b.broadcast(msg)
		if err != nil {
			return nil, fmt.Errorf("error broadcasting message %v %w", msg, err)
		}
	}
	return b.notifyCh, nil
}

func (b *BVBroadcast) HandleMessage(msg *typedefs.BVMessage) (int, bool, error) {
	if msg.BinValue != Zero && msg.BinValue != One && msg.BinValue != UndecidedBinVal {
		return 0, false, fmt.Errorf("invalid binary value %d from node %d", msg.BinValue, msg.SourceNode)
	}
	msg_binval := int(msg.BinValue)
	msg_source := int(msg.SourceNode)

	b.mu.Lock()
	if _, ok := b.received[msg_binval][msg_source]; ok {
		b.mu.Unlock()
		return 0, false, fmt.Errorf("redundant bv_broadcast from node %d", msg.SourceNode)
	}
	b.received[msg_binval][msg_source] = struct{}{}
	// fmt.Printf("num received at %d is %d\n", b.nodeID, b.received)

	shouldBroadcast := false
	if len(b.received[msg_binval]) >= b.threshold+1 && !b.broadcasted[msg.BinValue] {
		b.broadcasted[msg.BinValue] = true
		shouldBroadcast = true
	}
	b.mu.Unlock()

	if shouldBroadcast {
		echoMsg := &typedefs.BVMessage{SourceNode: int32(b.nodeID), BinValue: msg.BinValue, RoundId: msg.RoundId}
		err := b.broadcast(echoMsg)
		if err != nil {
			return 0, false, fmt.Errorf("error broadcasting message %v %w", msg, err)
		}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.received[msg_binval]) >= 2*b.threshold+1 && !b.BinValues.AsBools()[msg.BinValue] {
		b.BinValues.AddValue(msg_binval)

		b.notifyCh <- struct{}{}
		return msg_binval, true, nil
	}

	return 0, false, nil // TODO maybe return value not needed at all
}
