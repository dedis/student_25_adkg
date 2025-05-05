package agreement

import (
	"context"
	"student_25_adkg/networking"
	"student_25_adkg/transport/udp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func BVDefaultNetworkSetup() (
	networkIfaces []networking.NetworkInterface[[]byte],
	nParticipants int,
	threshold int,
	notifyChs []chan struct{},
	abaID string,
	bvInstances []*BVBroadcast,
	cancel context.CancelFunc,
) {
	nParticipants = 16
	threshold = 5
	notifyChs = make([]chan struct{}, nParticipants)
	abaID = "bv_test"
	bvInstances = make([]*BVBroadcast, nParticipants)

	network := networking.NewTransportNetwork(udp.NewUDP())

	ctx, cancel := context.WithCancel(context.Background())

	for i := 0; i < nParticipants; i++ {
		iface, err := network.JoinNetwork()
		if err != nil {
			panic(err)
		}
		abaStream := NewABAStream(iface)
		nodeConf := &ABACommonConfig{
			NParticipants: nParticipants,
			Threshold:     threshold,
			NodeID:        i,
			BroadcastFn:   abaStream.Broadcast,
		}
		abaNode := NewABAService(*nodeConf)
		abaStream.Listen(ctx, abaNode)
		networkIfaces = append(networkIfaces, abaStream.Iface)
		bvInstances[i] = abaNode.BVManager.GetOrCreate(abaID)
	}

	return
}

// Assume 3t+1 correct processes. Everyone broadcasts 1.
// Eventually all binvalues will contain 1.
// Getting notification on addition to binValues IS tested.
func TestABA_BVBroadcast_NotifySimple(t *testing.T) {

	networkIfaces, nParticipants, _, notifyChs, abaID, bvInstances, cancel := BVDefaultNetworkSetup()
	proposalVal := 1

	wg := sync.WaitGroup{}
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func(i int) {
			defer wg.Done()
			notifyCh, err := bvInstances[i].Propose(abaID, proposalVal)
			require.NoError(t, err)
			notifyChs[i] = notifyCh
		}(i)
	}

	// needed to make sure all chans of notifyChs are initialized
	wg.Wait()

	for i := range nParticipants {
		<-notifyChs[i]
	}

	// check binSet contains what it should
	for i := range nParticipants {
		require.True(t, bvInstances[i].BinValues.AsBools()[1])
		require.False(t, bvInstances[i].BinValues.AsBools()[0])
	}

	// check all messages are received
	for _, n := range networkIfaces {
		outs := n.GetReceived()
		bvMsgs, _, _, _, err := DecodeMessagesByType(outs)
		if err != nil {
			panic(err)
		}
		require.Equal(t, nParticipants, len(bvMsgs))
	}
	cancel()
}

// Assume 3t+1 correct processes.
// Nodes with even index broadcast 0, nodes with odd index broadcast 1.
// So t+1 different nodes broadcast 1 and other t+1 broadcast zero.
// Eventually all binvalues will contain both 0 and 1.
// Getting notification on addition to binValues IS NOT tested.
func TestABA_BVBroadcast_TwoValues(t *testing.T) {
	_, nParticipants, _, notifyChs, abaID, bvInstances, cancel := BVDefaultNetworkSetup()

	wg := sync.WaitGroup{}
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func(i int) {
			defer wg.Done()
			proposalVal := i % 2
			notifyCh, err := bvInstances[i].Propose(abaID, proposalVal)
			require.NoError(t, err)
			notifyChs[i] = notifyCh
		}(i)
	}

	// needed to make sure all chans of notifyChs are initialized
	wg.Wait()

	for i := range nParticipants {
		<-notifyChs[i]
		<-notifyChs[i]
	}

	// check binSet contains what it should
	for i := range nParticipants {
		require.True(t, bvInstances[i].BinValues.AsBools()[1])
		require.True(t, bvInstances[i].BinValues.AsBools()[0])
	}
	cancel()
}

// Assume t+1 correct processes which broadcast 1 while t byzantine broadcast 0.
// Eventually all binvalues of correct processes will contain 1.
// Getting notification on addition to binValues IS tested.
func TestABA_BVBroadcast_TByzantine_Success(t *testing.T) {
	_, nParticipants, threshold, notifyChs, abaID, bvInstances, cancel := BVDefaultNetworkSetup()

	correctVal := 1
	byzVal := 0

	// Sample t+1 correct and t byzantine processes
	randomProcesses, err := uniqueRandomInts(threshold*2+1, 0, nParticipants)
	require.NoError(t, err)

	correctIds := randomProcesses[:threshold+1]
	byzIds := randomProcesses[threshold+1:]

	wg := sync.WaitGroup{}
	wg.Add(len(correctIds) + len(byzIds))
	for _, pid := range correctIds {
		go func(pid int) {
			defer wg.Done()
			notifyCh, err := bvInstances[pid].Propose(abaID, correctVal)
			require.NoError(t, err)
			notifyChs[pid] = notifyCh
		}(pid)
	}

	for _, pid := range byzIds {
		go func(pid int) {
			defer wg.Done()
			notifyCh, err := bvInstances[pid].Propose(abaID, byzVal)
			require.NoError(t, err)
			notifyChs[pid] = notifyCh

		}(pid)
	}

	// needed to make sure all chans of notifyChs are initialized
	wg.Wait()

	// check only channels which were added to notifyChs
	for _, pid := range randomProcesses {
		<-notifyChs[pid]
	}

	// Verify that all correct nodes' binValues contain the correct value
	for _, pid := range correctIds {
		binValues := bvInstances[pid].BinValues.AsBools()
		require.NoError(t, err)
		require.True(t, binValues[1], "Node %d: binValues should contain 1", pid)
		require.False(t, binValues[0], "Node %d: binValues should not contain 0", pid)
	}

	cancel()
}

// Assume 2t+1 correct processes which DO NOT broadcast while t byzantine broadcast 0.
// Eventually all binvalues of correct processes should be EMPTY.
// Getting notification on addition to binValues IS tested.
func TestABA_BVBroadcast_TByzantine_Fail(t *testing.T) {
	_, nParticipants, threshold, notifyChs, abaID, bvInstances, cancel := BVDefaultNetworkSetup()

	byzVal := 0

	// Sample t+1 correct and t byzantine processes
	byzIds, err := uniqueRandomInts(threshold, 0, nParticipants)
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(len(byzIds))

	for _, pid := range byzIds {
		go func(pid int) {
			defer wg.Done()
			notifyCh, err := bvInstances[pid].Propose(abaID, byzVal)
			require.NoError(t, err)
			notifyChs[pid] = notifyCh

		}(pid)
	}

	// needed to make sure all chans of notifyChs are initialized
	wg.Wait()

	// sleep to make sure all communication rounds have finished
	time.Sleep(time.Millisecond * 15 * time.Duration(nParticipants))

	// Verify that all nodes' binValues are empty
	for _, pid := range byzIds {
		binValues := bvInstances[pid].BinValues.AsBools()
		require.NoError(t, err)
		require.False(t, binValues[0], "Node %d: binValues should not contain 0", pid)
		require.False(t, binValues[1], "Node %d: binValues should not contain 1", pid)
		require.False(t, binValues[2], "Node %d: binValues should not contain undecided value", pid)
	}

	cancel()
}
