package agreement

import (
	"context"
	"student_25_adkg/networking"
	"student_25_adkg/transport/udp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func SBVDefaultSetup() (
	networkIfaces []networking.NetworkInterface[[]byte],
	nParticipants int,
	threshold int,
	views [][3]bool,
	binValues [][3]bool,
	abaID string,
	sbvInstances []*SBVBroadcast,
	cancel context.CancelFunc,
) {
	network := networking.NewTransportNetwork(udp.NewUDP())
	ctx, cancel := context.WithCancel(context.Background())

	nParticipants = 16
	threshold = 5
	views = make([][3]bool, nParticipants)
	binValues = make([][3]bool, nParticipants)
	abaID = "sbv_test"
	sbvInstances = make([]*SBVBroadcast, nParticipants)

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
		sbvInstances[i] = abaNode.SBVManager.GetOrCreate(abaID)
	}

	return
}

// Assume 3t+1 correct processes. Everyone broadcasts 1.
// Eventually all binvalues will contain 1.
func TestABA_SBVBroadcast_Simple(t *testing.T) {

	netIfaces, nParticipants, _, views, binValues, abaID, sbvInstances, cancel := SBVDefaultSetup()
	proposalVal := 1

	wg := sync.WaitGroup{}
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func(pid int) {
			defer wg.Done()
			view, binSet, err := sbvInstances[pid].Propose(abaID, proposalVal)
			views[pid] = view
			binValues[pid] = binSet
			require.NoError(t, err)
		}(i)
	}

	// Wait for sbv broadcasts to complete
	wg.Wait()

	// Verify that all nodes' binValues and views contain the correct value
	for i := 0; i < nParticipants; i++ {
		require.True(t, binValues[i][1], "Node %d: binValues should contain 1", i)
		require.False(t, binValues[i][0], "Node %d: binValues should not contain 0", i)
		require.True(t, views[i][1], "Node %d: view should contain 1", i)
		require.False(t, views[i][0], "Node %d: view should not contain 0", i)
	}

	time.Sleep(time.Millisecond * 15 * time.Duration(nParticipants))
	// check all messages are received
	for _, n := range netIfaces {
		outs := n.GetReceived()
		bvMsgs, auxMessages, _, _, err := DecodeMessagesByType(outs)
		if err != nil {
			panic(err)
		}
		require.Equal(t, nParticipants, len(auxMessages))
		require.Equal(t, nParticipants, len(bvMsgs))
	}

	cancel()
}

// Test 2 – Honest disagreement: half send 0, half send 1
//   - n = 4, t = 1
//   - Inputs: [0, 1, 0, 1]
//   - Honest processes may see bin_values = {0, 1}
//   - Key test: make sure they don’t return early until the AUX predicate is satisfied
func TestABA_SBVBroadcast_TwoValues(t *testing.T) {
	_, nParticipants, _, views, binValues, abaID, sbvInstances, cancel := SBVDefaultSetup()

	wg := sync.WaitGroup{}
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func(pid int) {
			defer wg.Done()
			proposalVal := i % 2
			view, binSet, err := sbvInstances[pid].Propose(abaID, proposalVal)
			views[pid] = view
			binValues[pid] = binSet
			require.NoError(t, err)
		}(i)
	}

	// Wait for sbv broadcasts to complete
	wg.Wait()

	// Verify that all nodes' binValues and views contain the correct value
	for i := 0; i < nParticipants; i++ {
		require.True(t, binValues[i][0] || binValues[i][1], "Node %d: binValues could contain 0 or 1 or both", i)
		require.True(t, views[i][0] || views[i][1], "Node %d: view could contain 0 or 1 or both", i)
		require.False(t, views[i][2] || binValues[i][2], "Node %d: neither view nor binValues should not contain nonbinary values", i)
	}

	cancel()
}

// Test 3 – Faulty process stays silent
// 	•	n = 4, t = 1
// 	•	One node simply doesn’t send any AUX
// 	•	Others must still complete once n - t = 3 AUX messages are received
// 	•	Make sure the protocol does not wait forever

// sbv hangs if only t+1 (more precisely less than n-t) correct processes broadcast.
// Makes sense because then they don't send enough auxes...
// The process who has never started SBV (broadcasted, not just listening) will never broadcast aux.
// Looks correct according to pseudocode from the paper.
func TestABA_SBVBroadcast_SilentThreshold(t *testing.T) {
	_, nParticipants, threshold, views, binValues, abaID, sbvInstances, cancel := SBVDefaultSetup()
	proposalVal := 1

	correctIDs, err := uniqueRandomInts(threshold*2+1, 0, nParticipants)
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(len(correctIDs))
	for _, pid := range correctIDs {
		go func(pid int) {
			defer wg.Done()
			view, binSet, err := sbvInstances[pid].Propose(abaID, proposalVal)
			views[pid] = view
			binValues[pid] = binSet
			require.NoError(t, err)
		}(pid)
	}

	// Wait for sbv broadcasts to complete
	wg.Wait()

	// Verify that all nodes' binValues and views contain the correct value
	for _, pid := range correctIDs {
		require.True(t, !binValues[pid][0] && binValues[pid][1] && !binValues[pid][2], "Node %d: binValues should only contain 1", pid)
		require.True(t, !views[pid][0] && views[pid][1] && !views[pid][2], "Node %d: view should only contain 1", pid)
	}

	cancel()
}

// Test 4 – Faulty process sends conflicting AUX values
//   - Faulty node sends {0} to some, {1} to others
//   - Honest nodes must only consider the values they received
//   - Each honest process must still satisfy:
//   - view ⊆ bin_values
//   - values received from n - t distinct senders
func TestABA_SBVBroadcast_ByzantineAux(t *testing.T) {
	// network := networking.NewFakeNetwork[[]byte]()
	network := networking.NewTransportNetwork(udp.NewUDP())
	ctx, cancel := context.WithCancel(context.Background())

	nParticipants := 4
	threshold := 1
	views := make([][3]bool, nParticipants)
	binValues := make([][3]bool, nParticipants)
	abaID := "sbv_test"
	sbvInstances := make([]*SBVBroadcast, nParticipants)
	proposalVal := 1

	destNodes := make([]int, nParticipants)
	for i := 0; i < nParticipants; i++ {
		destNodes[i] = i + 1
	}
	for i := 0; i < nParticipants; i++ {
		iface, err := network.JoinNetwork()
		if err != nil {
			panic(err)
		}
		abaStream := NewABAStream(iface)

		var broadcastFn func(msg proto.Message) error
		if i < threshold {
			broadcastFn = abaStream.RandomSBVBroadcast(destNodes)
		} else {
			broadcastFn = abaStream.Broadcast
		}
		nodeConf := &ABACommonConfig{
			NParticipants: nParticipants,
			Threshold:     threshold,
			NodeID:        i,
			BroadcastFn:   broadcastFn,
		}
		abaNode := NewABAService(*nodeConf)
		abaStream.Listen(ctx, abaNode)
		sbvInstances[i] = abaNode.SBVManager.GetOrCreate(abaID)

	}

	wg := sync.WaitGroup{}
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func(pid int) {
			defer wg.Done()
			view, binSet, err := sbvInstances[pid].Propose(abaID, proposalVal)
			views[pid] = view
			binValues[pid] = binSet
			require.NoError(t, err)
		}(i)
	}

	// Wait for sbv broadcasts to complete
	wg.Wait()

	// Verify that all correct nodes' binValues and views contain the correct value
	for i := threshold; i < nParticipants; i++ {
		require.True(t, binValues[i][1], "Node %d: binValues should contain 1", i)
		require.False(t, binValues[i][0], "Node %d: binValues should not contain 0", i)
		require.True(t, views[i][1], "Node %d: view should contain 1", i)
		require.False(t, views[i][0], "Node %d: view should not contain 0", i)
	}

	cancel()
}
