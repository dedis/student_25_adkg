package agreement

import (
	"context"
	"strconv"
	"student_25_adkg/networking"
	"student_25_adkg/transport/disrupted"
	"student_25_adkg/transport/udp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/sign/tbls"
	"go.dedis.ch/kyber/v4/xof/blake2xb"
)

func ABADefaultSetup(network *networking.TransportNetwork) (
	nParticipants int,
	threshold int,
	abaInstances []*ABA,
	decidedVals []int,
	ctx context.Context,
	cancel context.CancelFunc,
	agreementID int,
) {
	nParticipants = 16
	threshold = 5
	agreementID = 1
	abaInstances = make([]*ABA, nParticipants)
	decidedVals = make([]int, nParticipants)

	ctx, cancel = context.WithCancel(context.Background())

	// localShare share.PriShare, pubCommitment *share.PubPoly for each coin
	seedBytes := []byte("Hello Common Coin {}")
	stream := blake2xb.New(seedBytes)
	suite := bn256.NewSuiteRand(stream)
	scheme := tbls.NewThresholdSchemeOnG1(suite)

	secret := suite.G1().Scalar().Pick(stream)
	priPoly := share.NewPriPoly(suite.G2(), threshold, secret, stream)
	pubPoly := priPoly.Commit(suite.G2().Point().Base())
	priShares := priPoly.Shares(nParticipants)

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
			Scheme:        scheme,
			LocalShare:    priShares[i],
			PubCommitment: pubPoly,
		}
		abaNode := NewABAService(*nodeConf)
		abaStream.Listen(ctx, abaNode)
		abaInstances[i] = abaNode.ABAManager.GetOrCreate(strconv.Itoa(agreementID))
	}
	return nParticipants,
		threshold,
		abaInstances,
		decidedVals,
		ctx,
		cancel,
		agreementID
}

func ABAsMultipleSetup(agreementIDs []int) (
	nParticipants int,
	threshold int,
	abaInstances []map[int]*ABA, // pid -> agreementID -> ABA
	decidedVals []map[int]int,
	ctx context.Context,
	cancel context.CancelFunc,
) {
	nParticipants = 4
	threshold = 1
	abaInstances = make([]map[int]*ABA, nParticipants)
	for pid := range abaInstances {
		abaInstances[pid] = make(map[int]*ABA)
	}
	// decidedVals = make([]int, nParticipants)
	decidedVals = make([]map[int]int, nParticipants)
	for pid := range decidedVals {
		decidedVals[pid] = make(map[int]int)
	}

	network := networking.NewTransportNetwork(udp.NewUDP())

	ctx, cancel = context.WithCancel(context.Background())

	// localShare share.PriShare, pubCommitment *share.PubPoly for each coin
	seedBytes := []byte("Hello Common Coin {}")
	stream := blake2xb.New(seedBytes)
	suite := bn256.NewSuiteRand(stream)
	scheme := tbls.NewThresholdSchemeOnG1(suite)

	secret := suite.G1().Scalar().Pick(stream)
	priPoly := share.NewPriPoly(suite.G2(), threshold, secret, stream)
	pubPoly := priPoly.Commit(suite.G2().Point().Base())
	priShares := priPoly.Shares(nParticipants)

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
			Scheme:        scheme,
			LocalShare:    priShares[i],
			PubCommitment: pubPoly,
		}
		abaNode := NewABAService(*nodeConf)
		abaStream.Listen(ctx, abaNode)
		for _, agrID := range agreementIDs {
			abaInstances[i][agrID] = abaNode.ABAManager.GetOrCreate(strconv.Itoa(agrID))
		}
	}
	return nParticipants,
		threshold,
		abaInstances,
		decidedVals,
		ctx,
		cancel
}

// Assume 3t+1 correct processes. Everyone broadcasts 1.
// Eventually everyone should decide 1.
func TestABA_Simple(t *testing.T) {

	network := networking.NewTransportNetwork(udp.NewUDP())
	nParticipants, _, abaInstances, decidedVals, _, cancel, _ := ABADefaultSetup(network)

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

	cancel()
}

// Assume 3t+1 correct processes.
// Nodes with even index broadcast 0, nodes with odd index broadcast 1.
// Nodes can't decide -> toss a coin.
// The test checks all ABA decided the same value.
// !!! Coin toss can not be guarantied but if it happens it is present in the logs.
// GLOG=debug go test -run TestABA_WithCoin  -v -race  -count 1
func TestABA_WithCoin(t *testing.T) {

	network := networking.NewTransportNetwork(udp.NewUDP())
	nParticipants, _, abaInstances, decidedVals, _, cancel, _ := ABADefaultSetup(network)

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

	// check that all nodes decided the same value (coin or not coin)
	decidedVal := decidedVals[0]
	for i := 1; i < nParticipants; i++ {
		require.Equal(t, decidedVal, decidedVals[i], "Node %d should have decided %a", i, decidedVal)
		decidedVal = decidedVals[i]
	}

	cancel()
}

// Run multiple ABAs at the same time.
// Make sure they all decide correct value.
func TestABA_Multiple_Simple(t *testing.T) {
	agreementIDs := []int{1, 2, 3}
	nParticipants, _, abaInstances, decidedVals, _, cancel := ABAsMultipleSetup(agreementIDs)
	var decideMu sync.Mutex

	proposalVal := 1
	wg := sync.WaitGroup{}
	wg.Add(nParticipants * len(agreementIDs))
	for i := 0; i < nParticipants; i++ {
		for _, agrID := range agreementIDs {
			go func(pid int) {
				defer wg.Done()
				decision, err := abaInstances[pid][agrID].Propose(proposalVal)
				decideMu.Lock()
				decidedVals[i][agrID] = decision
				decideMu.Unlock()
				require.NoError(t, err)
			}(i)
		}
	}

	// Wait for aba to complete at each node
	wg.Wait()

	// Verify that all nodes' decided the correct value for each aba
	for _, agrID := range agreementIDs {
		for i := 0; i < nParticipants; i++ {
			require.Equal(t, proposalVal, decidedVals[i][agrID], "Node %d should have decided %a", i, proposalVal)
		}
	}

	cancel()
}

func TestABA_WithCoin_Disrupted(t *testing.T) {
	getTestDisrupted := func(disruptedLayer *disrupted.Transport) func(*testing.T) {
		disruptedLayer.SetRandomGenSeed(1)
		return func(t *testing.T) {

			network := networking.NewTransportNetwork(disruptedLayer)
			nParticipants, _, abaInstances, decidedVals, _, cancel, _ := ABADefaultSetup(network)

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

			// check that all nodes decided the same value (coin or not coin)
			decidedVal := decidedVals[0]
			for i := 1; i < nParticipants; i++ {
				require.Equal(t, decidedVal, decidedVals[i], "Node %d should have decided %a", i, decidedVal)
				decidedVal = decidedVals[i]
			}

			cancel()
		}
	}
	t.Run("UDP transport with a normal bridge node",
		getTestDisrupted(disrupted.NewDisrupted(udp.NewUDP())))
	t.Run("UDP transport with a slightly jammed bridge node",
		getTestDisrupted(disrupted.NewDisrupted(udp.NewUDP(), disrupted.WithJam(1*time.Second, 2))))
	t.Run("UDP transport with a heavily jammed bridge node",
		getTestDisrupted(disrupted.NewDisrupted(udp.NewUDP(), disrupted.WithJam(2*time.Second, 10))))
	t.Run("UDP transport with a single delayed node",
		getTestDisrupted(disrupted.NewDisrupted(udp.NewUDP(), disrupted.WithFixedDelay(500*time.Millisecond))))
}
