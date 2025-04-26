package agreement

import (
	"context"
	"strconv"
	"student_25_adkg/networking"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func ABADefaultSetup() (
	nParticipants int,
	threshold int,
	abaInstances []*ABA,
	decidedVals []int,
	ctx context.Context,
	cancel context.CancelFunc,
	agreementID int,
) {
	nParticipants = 4
	threshold = 1
	agreementID = 1
	abaInstances = make([]*ABA, nParticipants)
	decidedVals = make([]int, nParticipants)

	network := networking.NewFakeNetwork[[]byte]()

	ctx, cancel = context.WithCancel(context.Background())

	for i := 0; i < nParticipants; i++ {
		iface := network.JoinNetwork()
		abaStream := NewABAStream(iface)
		nodeConf := &ABACommonConfig{
			NParticipants: nParticipants,
			Threshold:     threshold,
			NodeID:        i,
			BroadcastFn:   abaStream.Broadcast,
		}
		abaNode := NewABANode(*nodeConf)
		abaStream.Listen(ctx, abaNode)
		abaInstances[i] = abaNode.ABAManager.GetOrCreate(strconv.Itoa(agreementID))
	}
	return
}

func ABANetworkSetup([]*ABA) {

}

// Assume 3t+1 correct processes. Everyone broadcasts 1.
// Eventually everyone should decide 1.
func TestABA_Simple(t *testing.T) {

	nParticipants, _, abaInstances, decidedVals, _, cancel, _ := ABADefaultSetup()

	proposalVal := 1
	wg := sync.WaitGroup{}
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func(pid int) {
			defer wg.Done()
			var err error
			decidedVals[i], err = abaInstances[pid].Propose(proposalVal)
			require.NoError(t, err)
		}(i)
	}

	// Wait for aba to complete at each node
	wg.Wait()

	// Verify that all nodes' decided the correct value
	for i := 0; i < nParticipants; i++ {
		require.Equal(t, proposalVal, decidedVals[i], "Node %d should have decided %a", i, proposalVal)
	}
	// TODO check all messages are sent

	cancel()
}

// Assume 3t+1 correct processes. Everyone broadcasts 1.
// Nodes with even index broadcast 0, nodes with odd index broadcast 1.
// Nodes can't decide -> evoke a coin.
// !!! Now can only see it in the logs.
// ❯ GLOG=debug go test -run TestABA_WithCoin  -v -race  -count 1
func TestABA_WithCoin(t *testing.T) {

	nParticipants, _, abaInstances, decidedVals, _, cancel, _ := ABADefaultSetup()

	wg := sync.WaitGroup{}
	wg.Add(nParticipants)
	for i := 0; i < nParticipants; i++ {
		go func(pid int) {
			defer wg.Done()
			var err error
			proposalVal := i % 2
			decidedVals[i], err = abaInstances[pid].Propose(proposalVal)
			require.NoError(t, err)
		}(i)
	}

	// Wait for aba to complete at each node
	wg.Wait()
	// TODO check all messages are sent

	cancel()
}
