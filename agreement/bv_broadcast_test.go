package agreement

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TODO: setup function, Theo's FakeNetwork
type TestNetwork struct {
	bvNodes        []*BVBroadcast
	execConcurrent bool
	wg             sync.WaitGroup
}

func NewBroadcastSimulator(execConcurrent bool) *TestNetwork {
	return &TestNetwork{
		bvNodes:        []*BVBroadcast{},
		execConcurrent: execConcurrent,
	}
}

func (n *TestNetwork) AddNode(node *BVBroadcast) {
	n.bvNodes = append(n.bvNodes, node)
}

func (n *TestNetwork) DisseminateBVMsg(msg IMessage) error {
	if bvMsg, ok := msg.(*BVMessage); ok {
		if n.execConcurrent {
			n.wg.Add(len(n.bvNodes))
			for i := 0; i < len(n.bvNodes); i++ {
				go func(node *BVBroadcast) {
					defer n.wg.Done()
					_, _, err := node.HandleMessage(bvMsg)
					if err != nil {
						fmt.Printf("error handling bvMsg %v: %v\n", msg, err)
					}
				}(n.bvNodes[i])
			}
		} else {
			for i := 0; i < len(n.bvNodes); i++ {
				_, _, err := n.bvNodes[i].HandleMessage(bvMsg)
				if err != nil {
					return fmt.Errorf("error handling bvMsg %v %w", msg, err)
				}
			}
		}
		return nil
	}
	return fmt.Errorf("wrong msg type, BVMessage expected")
}

func uniqueRandomInts(n, min, max int) ([]int, error) {
	if max-min < n {
		return nil, fmt.Errorf("range too small for %d unique values", n)
	}

	// create slice of all values
	pool := make([]int, max-min)
	for i := range pool {
		pool[i] = min + i
	}

	rand.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})

	return pool[:n], nil
}

// Assume 3t+1 correct processes. Everyone broadcasts 1.
// Eventually all binvalues will contain 1.
// This test assumes all operations are are happenning serially on the same machine.
// So handleMessage of node 1 may call handleMessage of node x through broadcast and
// eventually handleMessage of node 1 will be called again.
// This is not a real scenario but it can be seen as an extreme anti-deadlock test.
func TestBVBroadcastSerialSimple(t *testing.T) {
	nParticipants := 4
	threshold := 1
	execConcurrent := false
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)

	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg)
		broadcastSimulator.AddNode(bvInstance)
	}

	for i := 0; i < nParticipants; i++ {
		bvMsg := &BVMessage{sourceNode: i, binValue: 1}
		_, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg)
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

// Assume 3t+1 correct processes. Everyone broadcasts 1.
// Eventually all binvalues will contain 1.
// Getting notification on addition to binValues is NOT tested.
func TestBVBroadcastSimple(t *testing.T) {
	nParticipants := 4
	threshold := 1
	execConcurrent := true
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)

	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg)
		broadcastSimulator.AddNode(bvInstance)
	}

	broadcastSimulator.wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func() {
			defer broadcastSimulator.wg.Done()
			bvMsg := &BVMessage{sourceNode: i, binValue: 1}
			_, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg)
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}()
	}

	// wait for messageHandler goroutines to finish
	broadcastSimulator.wg.Wait()

	// check binSet contains what it should
	for i := range nParticipants {
		// t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}
	// TODO check all messages are sent
}

// Assume 3t+1 correct processes. Everyone broadcasts 1.
// Eventually all binvalues will contain 1.
// Getting notification on addition to binValues IS tested.
func TestBVBroadcastNotifySimple(t *testing.T) {
	nParticipants := 4
	threshold := 1
	execConcurrent := true
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)
	var notifyChs []chan struct{}
	var m sync.Mutex

	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg)
		broadcastSimulator.AddNode(bvInstance)
	}

	var wg sync.WaitGroup
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func() {
			defer wg.Done()
			bvMsg := &BVMessage{sourceNode: i, binValue: 1}
			notifyCh, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg)
			m.Lock()
			notifyChs = append(notifyChs, notifyCh)
			m.Unlock()
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}()
	}
	// wait for notifyChs to be populated
	wg.Wait()

	// don't wait for goroutines to finish
	// instead wait for explicit notification from handlers
	// which is triggered on addition to binValues set
	for i := range nParticipants {
		<-notifyChs[i]
	}

	// check binSet contains what it should
	for i := range nParticipants {
		// t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}

	// TODO check all messages are sent

	// make sure all handlers eventually finished
	broadcastSimulator.wg.Wait()
}

// Assume 3t+1 correct processes.
// Nodes with even index broadcast 0, nodes with odd index broadcast 1.
// So t+1 different nodes broadcast 1 and other t+1 broadcast zero.
// Eventually all binvalues will contain both 0 and 1.
// Getting notification on addition to binValues IS NOT tested.
func TestBVBroadcastTwoValues(t *testing.T) {
	nParticipants := 4
	threshold := 1
	execConcurrent := true
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)

	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg)
		broadcastSimulator.AddNode(bvInstance)
	}

	broadcastSimulator.wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func() {
			defer broadcastSimulator.wg.Done()
			bvMsg := &BVMessage{sourceNode: i, binValue: i % 2}
			_, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg)
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}()
	}

	// wait for messageHandler goroutines to finish
	broadcastSimulator.wg.Wait()

	// check binSet contains what it should
	for i := range nParticipants {
		// t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}
	//TODO check all messages are sent
}

// Assume 3t+1 correct processes.
// Nodes with even index broadcast 0, nodes with odd index broadcast 1.
// So t+1 different nodes broadcast 1 and other t+1 broadcast zero.
// Eventually all binvalues will contain both 0 and 1.
// Getting notification on addition to binValues IS tested.
func TestBVBroadcastNotifyTwoValues(t *testing.T) {
	nParticipants := 4
	threshold := 1
	execConcurrent := true
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)
	var notifyChs []chan struct{}
	var m sync.Mutex

	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg)
		broadcastSimulator.AddNode(bvInstance)
	}

	var wg sync.WaitGroup
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func() {
			defer wg.Done()
			bvMsg := &BVMessage{sourceNode: i, binValue: i % 2}
			notifyCh, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg)
			m.Lock()
			notifyChs = append(notifyChs, notifyCh)
			m.Unlock()
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}()
	}
	// wait for notifyChs to be populated
	wg.Wait()

	for i := range nParticipants {
		<-notifyChs[i]
		<-notifyChs[i]
	}

	// check binSet contains what it should
	for i := range nParticipants {
		// t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}

	// TODO check all messages are sent

	// make sure all handlers eventually finished
	broadcastSimulator.wg.Wait()
}

// Assume t+1 correct processes which broadcast 1 while t byzantine broadcast 0.
// Eventually all binvalues of correct processes will contain 1.
// Getting notification on addition to binValues IS tested.
func TestBVBroadcastTByzantineSuccess(t *testing.T) {
	nParticipants := 16
	threshold := 5
	execConcurrent := true
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)

	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg)
		broadcastSimulator.AddNode(bvInstance)
	}

	correctVal := 1
	byzVal := 0

	// Sample t+1 correct and t byzantine processes
	randomProcesses, err := uniqueRandomInts(threshold*2+1, 0, nParticipants)
	if err != nil {
		t.Log(err)
	}
	correctIds := randomProcesses[:threshold+1]
	byzIds := randomProcesses[threshold+1:]

	wg := sync.WaitGroup{}
	wg.Add(len(correctIds) + len(byzIds))

	// Correct processes broadcast correctVal
	for _, pid := range correctIds {
		go func(pid int) {
			defer wg.Done()
			bvMsg := &BVMessage{sourceNode: pid, binValue: correctVal}
			_, err := broadcastSimulator.bvNodes[pid].Broadcast(bvMsg)
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}(pid)
	}

	// Byzantine processes broadcast byzVal
	for _, pid := range byzIds {
		go func(pid int) {
			defer wg.Done()
			bvMsg := &BVMessage{sourceNode: pid, binValue: byzVal}
			_, err := broadcastSimulator.bvNodes[pid].Broadcast(bvMsg)
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}(pid)
	}

	// wait for broadcasts to start
	wg.Wait()

	// make sure all handlers eventually finished
	// so binValues of all nodes are final
	broadcastSimulator.wg.Wait()

	// Check results
	for i := 0; i < len(correctIds); i++ {
		// t.Logf("BinValues at node %d: %v", correctIds[i], broadcastSimulator.bvNodes[correctIds[i]].BinValues.AsInts())
		require.True(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}
}

// Assume 2t+1 correct processes which DO NOT broadcast while t byzantine broadcast 0.
// Eventually all binvalues of correct processes should be EMPTY.
// Getting notification on addition to binValues IS tested.
func TestBVBroadcastTByzantineFail(t *testing.T) {
	nParticipants := 16
	threshold := 5
	execConcurrent := true
	broadcastSimulator := NewBroadcastSimulator(execConcurrent)

	byzVal := 0
	byzIds, err := uniqueRandomInts(threshold, 0, nParticipants)
	if err != nil {
		t.Log(err)
	}

	for i := 0; i < nParticipants; i++ {
		bvInstance := NewBVBroadcast(nParticipants, threshold, i, broadcastSimulator.DisseminateBVMsg)
		broadcastSimulator.AddNode(bvInstance)
	}

	var wg sync.WaitGroup
	wg.Add(threshold)
	for _i := range byzIds {
		i := byzIds[_i]
		go func() {
			defer wg.Done()
			bvMsg := &BVMessage{sourceNode: i, binValue: byzVal}
			_, err := broadcastSimulator.bvNodes[i].Broadcast(bvMsg)
			if err != nil {
				t.Logf("error broadcasting message %v %s", bvMsg, err.Error())
			}
		}()
	}
	// wait for notifyChs to be populated
	wg.Wait()

	// make sure all handlers eventually finished
	// so binValues of all nodes are final
	broadcastSimulator.wg.Wait()

	// check binSet contains what it should
	for i := range nParticipants {
		// t.Logf("BinValues at node %d: %v", i, broadcastSimulator.bvNodes[i].BinValues.AsInts())
		require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[1])
		require.False(t, broadcastSimulator.bvNodes[i].BinValues.AsBools()[0])
	}
	// TODO check all messages are sent
}
