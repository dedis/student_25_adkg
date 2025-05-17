package agreement

import (
	"context"
	"student_25_adkg/networking"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/sign"
	"go.dedis.ch/kyber/v4/sign/tbls"
	"go.dedis.ch/kyber/v4/xof/blake2xb"
)

type TestCoin struct {
	LocalShare    *share.PriShare
	PubCommitment *share.PubPoly
	Scheme        sign.ThresholdScheme
}

func TestABA_CommonCoin_MockCoin(t *testing.T) {
	nParticipants := 4
	threshold := 3
	coinCombs := findCombinations(nParticipants, threshold)
	coinSigs := [][]byte{}

	coinMsg := "Common coin msg"
	seedBytes := []byte("Hello Common Coin")
	stream := blake2xb.New(seedBytes)
	suite := bn256.NewSuiteRand(stream)
	scheme := tbls.NewThresholdSchemeOnG1(suite)

	secret := suite.G1().Scalar().Pick(stream)
	priPoly := share.NewPriPoly(suite.G2(), threshold, secret, stream)
	pubPoly := priPoly.Commit(suite.G2().Point().Base())

	coins := make([]*TestCoin, nParticipants)
	for i, share := range priPoly.Shares(nParticipants) {
		localSeedBytes := []byte("Hello Common Coin")
		localStream := blake2xb.New(localSeedBytes)
		localSuite := bn256.NewSuiteRand(localStream)
		localScheme := tbls.NewThresholdSchemeOnG1(localSuite)

		coins[i] = &TestCoin{
			LocalShare:    share,
			PubCommitment: pubPoly,
			Scheme:        localScheme,
		}
	}

	sigShares := make([][]byte, nParticipants)
	for i, coin := range coins {
		sig, err := coin.Scheme.Sign(coin.LocalShare, []byte(coinMsg))
		require.Nil(t, err)
		require.Nil(t, coin.Scheme.VerifyPartial(coin.PubCommitment, []byte(coinMsg), sig))
		idx, err := coin.Scheme.IndexOf(sig)
		require.NoError(t, err)
		require.Equal(t, int(coin.LocalShare.I), idx)
		sigShares[i] = sig
	}

	for _, combIdxs := range coinCombs {
		combShares := make([][]byte, 0)
		for _, shareIdx := range combIdxs {
			combShares = append(combShares, sigShares[shareIdx])
		}

		sig, err := scheme.Recover(pubPoly, []byte(coinMsg), combShares, threshold, nParticipants)
		require.Nil(t, err)
		err = scheme.VerifyRecovered(pubPoly.Commit(), []byte(coinMsg), sig)
		require.Nil(t, err)
		coinSigs = append(coinSigs, sig)
	}

	prevCoinBit := coinSigs[0]
	for i := range coinCombs {
		require.Equal(t, prevCoinBit, coinSigs[i], "reconstructed sigs of all combination are the same")
	}
}

func TestABA_CommonCoin_Simple(t *testing.T) {
	network := networking.NewFakeNetwork()

	ctx, cancel := context.WithCancel(context.Background())

	nParticipants := 4
	threshold := 1
	abaID := "coin_test"
	// notifyChs := make([]chan int, nParticipants)
	coinSigBits := make([]int, nParticipants)
	coins := make([]*CommonCoin, nParticipants)

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
			LocalShare:    priShares[i],
			PubCommitment: pubPoly,
			Scheme:        scheme,
		}
		abaNode := NewABAService(*nodeConf)
		coins[i] = abaNode.CCoinManageer.GetOrCreate(abaID)
		abaStream.Listen(ctx, abaNode)
	}

	wg := sync.WaitGroup{}
	wg.Add(nParticipants)
	for pid := range nParticipants {
		go func(pid int) {
			defer wg.Done()
			coinSigBit, err := coins[pid].Flip() // flip takes in round, should be deleted
			require.NoError(t, err)
			coinSigBits[pid] = coinSigBit
		}(pid)
	}
	wg.Wait()

	// Verify that all correct nodes' coin outputs match
	prevCoinBit := coinSigBits[0]
	for pid := range nParticipants {
		require.Equal(t, prevCoinBit, coinSigBits[pid], "Node %d: coind values of all participants are the same", pid)
	}

	cancel()
}

// test ideas from rust:
// filters_invalid_share
// too_many_invalid_shares
