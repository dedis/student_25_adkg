package agreement

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type TestNetwork struct {
	bvNodes []*BVBroadcast
}

func NewBroadcastSimulator() *TestNetwork {
	return &TestNetwork{
		bvNodes: []*BVBroadcast{},
	}
}

func (bs *TestNetwork) AddNode(node *BVBroadcast) {
	bs.bvNodes = append(bs.bvNodes, node)
}

func (bs *TestNetwork) DisseminateBVMsg(msg IMessage) error {
	if bvMsg, ok := msg.(*BVMessage); ok {
		for i := 0; i < len(bs.bvNodes); i++ {
			// include myself?
			// if bvMsg.sourceNode == i {
			// 	continue
			// }
			_, _, err := bs.bvNodes[i].HandleMessage(bvMsg)
			if err != nil {
				return fmt.Errorf("error handling bvMsg %v %w", msg, err)
			}
		}
		return nil
	}
	return fmt.Errorf("wrong msg type, BVMessage expected")
}

func TestBVBroadcastSimple(t *testing.T) {
	nParticipants := 4
	threshold := 1
	broadcastSimulator := NewBroadcastSimulator()

	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg)
		broadcastSimulator.AddNode(bvInstance)
	}

	for i := 0; i < nParticipants; i++ {
		bvMsg := &BVMessage{sourceNode: i, binValue: 1}
		_, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg, false)
		if err != nil {
			t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
		}
	}

	for i := range nParticipants {
		t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}
	// check all messages are sent
	// check binSet contains what it should
}
