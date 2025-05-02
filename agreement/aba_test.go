package agreement

import (
	"context"
	"fmt"
	"strconv"
	"student_25_adkg/networking"
	"student_25_adkg/transport/disrupted"
	"student_25_adkg/transport/tcp"
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

func ABADefaultSetup() (
	nParticipants int,
	threshold int,
	abaInstances []*ABA,
	decidedVals []int,
	ctx context.Context,
	cancel context.CancelFunc,
	agreementID int,
) {
	nParticipants = 10
	threshold = 3
	agreementID = 1
	abaInstances = make([]*ABA, nParticipants)
	decidedVals = make([]int, nParticipants)

	// network := networking.NewFakeNetwork[[]byte]()
	// network := networking.NewTransportNetwork(udp.NewUDP())
	// network := networking.NewTransportNetwork(disrupted.NewDisrupted(udp.NewUDP(), disrupted.WithFixedDelay(500*time.Millisecond)))
	// network := networking.NewTransportNetwork(disrupted.NewDisrupted(udp.NewUDP(), disrupted.WithJam(time.Second, 16)))
	network := networking.NewTransportNetwork(disrupted.NewDisrupted(tcp.NewTCP(), disrupted.WithFixedDelay(500*time.Millisecond)))
	// network := networking.NewTransportNetwork(disrupted.NewDisrupted(tcp.NewTCP(), disrupted.WithJam(time.Second, 16)))

	ctx, cancel = context.WithCancel(context.Background())

	// localShare share.PriShare, pubCommitment *share.PubPoly for each coin
	seedBytes := []byte(fmt.Sprintf("Hello Common Coin {}"))
	stream := blake2xb.New(seedBytes)
	suite := bn256.NewSuiteRand(stream)
	scheme := tbls.NewThresholdSchemeOnG1(suite)

	secret := suite.G1().Scalar().Pick(stream)
	priPoly := share.NewPriPoly(suite.G2(), threshold, secret, stream)
	pubPoly := priPoly.Commit(suite.G2().Point().Base())
	priShares := priPoly.Shares(nParticipants)

	for i := 0; i < nParticipants; i++ {
		// iface := network.JoinNetwork()
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
		abaNode := NewABANode(*nodeConf)
		abaStream.Listen(ctx, abaNode)
		abaInstances[i] = abaNode.ABAManager.GetOrCreate(strconv.Itoa(agreementID))
	}
	return
}

func ABAsMultipleSetup(agreementIDs []int) (
	nParticipants int,
	threshold int,
	abaInstances []map[int]*ABA, // pid -> agreementID -> ABA
	decidedVals []int,
	ctx context.Context,
	cancel context.CancelFunc,
) {
	nParticipants = 4
	threshold = 1
	abaInstances = make([]map[int]*ABA, nParticipants)
	for pid := range abaInstances {
		abaInstances[pid] = make(map[int]*ABA)
	}
	decidedVals = make([]int, nParticipants)

	// network := networking.NewFakeNetwork[[]byte]()
	network := networking.NewTransportNetwork(udp.NewUDP())
	// network := networking.NewTransportNetwork(disrupted.NewDisrupted(udp.NewUDP(), disrupted.WithFixedDelay(500*time.Millisecond)))
	// network := networking.NewTransportNetwork(disrupted.NewDisrupted(udp.NewUDP(), disrupted.WithJam(time.Second, 16)))

	ctx, cancel = context.WithCancel(context.Background())

	// localShare share.PriShare, pubCommitment *share.PubPoly for each coin
	seedBytes := []byte(fmt.Sprintf("Hello Common Coin {}"))
	stream := blake2xb.New(seedBytes)
	suite := bn256.NewSuiteRand(stream)
	scheme := tbls.NewThresholdSchemeOnG1(suite)

	secret := suite.G1().Scalar().Pick(stream)
	priPoly := share.NewPriPoly(suite.G2(), threshold, secret, stream)
	pubPoly := priPoly.Commit(suite.G2().Point().Base())
	priShares := priPoly.Shares(nParticipants)

	for i := 0; i < nParticipants; i++ {
		// iface := network.JoinNetwork()
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
		abaNode := NewABANode(*nodeConf)
		abaStream.Listen(ctx, abaNode)
		for _, agrID := range agreementIDs {
			abaInstances[i][agrID] = abaNode.ABAManager.GetOrCreate(strconv.Itoa(agrID))
		}
	}
	return
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

func TestABA_Multiple_Simple(t *testing.T) {
	agreementIDs := []int{1, 2, 3}

	nParticipants, _, abaInstances, decidedVals, _, cancel := ABAsMultipleSetup(agreementIDs)

	proposalVal := 1
	wg := sync.WaitGroup{}
	wg.Add(nParticipants * len(agreementIDs))
	for i := 0; i < nParticipants; i++ {
		for _, agrID := range agreementIDs {
			go func(pid int) {
				defer wg.Done()
				var err error
				decidedVals[i], err = abaInstances[pid][agrID].Propose(proposalVal)
				require.NoError(t, err)
			}(i)
		}
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

// WithPayloadRandomizer
// WithPacketIDRandomizer not applicable
// WithSourceSpoofer
// WithGenericDelay
// WithLossSocket
