package agreement

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type TestNetwork struct {
	bvNodes        []*BVBroadcast
	execConcurrent bool
}

func NewBroadcastSimulator(execConcurrent bool) *TestNetwork {
	return &TestNetwork{
		bvNodes:        []*BVBroadcast{},
		execConcurrent: execConcurrent,
	}
}

func (bs *TestNetwork) AddNode(node *BVBroadcast) {
	bs.bvNodes = append(bs.bvNodes, node)
}

func (bs *TestNetwork) DisseminateBVMsg(msg IMessage) error {
	if bvMsg, ok := msg.(*BVMessage); ok {
		wait := sync.WaitGroup{}
		wait.Add(len(bs.bvNodes))

		if bs.execConcurrent {
			for i := 0; i < len(bs.bvNodes); i++ {
				go func() {
					wait.Done()
					_, _, err := bs.bvNodes[i].HandleMessage(bvMsg)
					if err != nil {
						panic(fmt.Errorf("error handling bvMsg %v %w", msg, err))
					}
				}()
			}
			wait.Wait()
		} else {
			for i := 0; i < len(bs.bvNodes); i++ {
				_, _, err := bs.bvNodes[i].HandleMessage(bvMsg)
				if err != nil {
					return fmt.Errorf("error handling bvMsg %v %w", msg, err)
				}
			}
		}
	}
	return nil
}

func TestBVBroadcastSerialSimple(t *testing.T) {
	nParticipants := 4
	threshold := 1
	execConcurrent := false
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)

	shouldNotify := false
	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg, shouldNotify)
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
		// t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}
	// check all messages are sent

}

func TestBVBroadcastSimple(t *testing.T) {
	nParticipants := 4
	threshold := 1
	execConcurrent := true
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)

	shouldNotify := false
	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg, shouldNotify)
		broadcastSimulator.AddNode(bvInstance)
	}

	wait := sync.WaitGroup{}
	wait.Add(nParticipants)

	for i := 0; i < nParticipants; i++ {
		go func() {
			defer wait.Done()
			bvMsg := &BVMessage{sourceNode: i, binValue: 1}
			_, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg, false)
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}()
	}

	wait.Wait()

	// time.Sleep(time.Millisecond * 50)
	// wait for goroutines to finish

	for i := range nParticipants {
		t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}
	// check all messages are sent
	// check binSet contains what it should
}

func TestBVBroadcastNotifySimple(t *testing.T) {
	nParticipants := 4
	threshold := 1
	execConcurrent := true
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)
	var notifyChs []chan struct{}

	shouldNotify := true
	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg, shouldNotify)
		broadcastSimulator.AddNode(bvInstance)
	}

	wait := sync.WaitGroup{}
	wait.Add(nParticipants)

	for i := 0; i < nParticipants; i++ {
		go func() {
			defer wait.Done()
			bvMsg := &BVMessage{sourceNode: i, binValue: 1}
			notifyCh, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg, shouldNotify)
			notifyChs = append(notifyChs, notifyCh)
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}()
	}

	wait.Wait()

	// wait = sync.WaitGroup{}
	// wait.Add(nParticipants)

	for i := range nParticipants {
		// go func() {
		// defer wait.Done()
		fmt.Printf("waiting for notification from %d\n", i)
		<-notifyChs[i]
		// }()
	}
	// wait.Wait()

	for i := range nParticipants {
		t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		// require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		// require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}
	// check all messages are sent
	// check binSet contains what it should
}
