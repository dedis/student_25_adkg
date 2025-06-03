package commoncoin

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/pairing/bn254"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
)

func findScalarWithFirstBit(
	suite Suite,
	generator kyber.Point,
	wantBit CoinToss) (kyber.Scalar, error) {
	iterCount := 0
	maxIters := 100

	var secret kyber.Scalar
	for {
		secret = suite.Scalar().Pick(suite.RandomStream())
		pubCommit := suite.Point().Mul(secret, generator)
		hash := suite.Hash()
		_, err := pubCommit.MarshalTo(hash)
		if err != nil {
			panic(err)
		}
		hashGTildeZero := hash.Sum(nil)
		if CoinToss((hashGTildeZero[0]>>7)&1) == wantBit {
			break
		}
		if iterCount > maxIters {
			return secret, fmt.Errorf("can't find a suitable scalar for too many iteration")
		}
		iterCount++
	}

	return secret, nil
}

func samplePolynomials(
	suite Suite,
	k, n int,
	secret kyber.Scalar,
	randomGGenerator kyber.Point,
) ([]*share.PriShare, []*share.PubShare) {
	priPoly := share.NewPriPoly(suite, k, secret, suite.RandomStream())
	priShares := priPoly.Shares(n)
	pubPoly := priPoly.Commit(randomGGenerator)
	pubShares := pubPoly.Shares(n)
	return priShares, pubShares
}

func createCoins(
	suite Suite,
	coinID string,
	randomGGenerator kyber.Point,
	priShares []*share.PriShare,
	pubShares []*share.PubShare,
	k, n int,
) ([]*ECDHCommonCoin, error) {

	coins := make([]*ECDHCommonCoin, n)
	for i := 0; i < n; i++ {
		coin, err := NewECDHCommonCoin(
			coinID,
			suite,
			randomGGenerator,
			pubShares,
			priShares[i],
			uint32(i),
			k,
			n,
		)
		if err != nil {
			return []*ECDHCommonCoin{}, err
		}
		coins[i] = coin
	}
	return coins, nil
}

func distributeShares(coinNodes []*ECDHCommonCoin) error {
	for _, c1 := range coinNodes {
		coinShare, err := c1.LocalShare()

		if err != nil {
			return err
		}
		for _, c2 := range coinNodes {
			err = c2.AddShare(coinShare)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func tossCoins(coinNodes []*ECDHCommonCoin) ([]CoinToss, error) {
	coinVals := []CoinToss{}
	for _, coin := range coinNodes {
		coinVal, err := coin.Toss()
		if err != nil {
			return []CoinToss{}, err
		}
		coinVals = append(coinVals, coinVal)
	}
	return coinVals, nil
}

func nextCoinsRound(coinNodes []*ECDHCommonCoin, baseID string, round int) error {
	coinID := baseID + strconv.Itoa(round)
	for _, coin := range coinNodes {
		err := coin.ResetCoinID(coinID)
		if err != nil {
			return err
		}
	}
	return nil
}

const CoinMsgText = "HelloCoin"

func TestCommonCoin_Simple(test *testing.T) {
	// n participants (coins), threshold k (k shares to reconstruct)

	// 1. Simulate dealing phase:
	// 		create global random generator of G, sample n x_i private shares
	// 		and g^x_i verification keys.

	// 2. create n common coins
	// 3. each coin does LocalShare (generates a coin share dependant on the coin text and secret key)
	//    each coin verifies all proofs of all other coins: require no errors
	//    each coin does a coin Toss (reconstruct the gTilde^x_0 and take first bit of its hash): require same values

	// 1
	n := 20
	k := 7

	coins := make([]*ECDHCommonCoin, n)
	suite := bn254.NewSuiteG1()

	priPoly := share.NewPriPoly(suite, k, nil, suite.RandomStream())
	priShares := priPoly.Shares(n)

	// randomGGenerator is the global VK sampled by the dealer
	randomGGenerator := suite.Point().Pick(random.New())
	pubPoly := priPoly.Commit(randomGGenerator)
	pubShares := pubPoly.Shares(n)

	// 2
	for i := range n {
		coinID := fmt.Sprintf("%s%d", CoinMsgText, 1)
		coin, err := NewECDHCommonCoin(
			coinID,
			suite,
			randomGGenerator,
			pubShares,
			priShares[i],
			uint32(i),
			k,
			n,
		)
		require.NoError(test, err)
		coins[i] = coin
	}

	// 3
	for i := range n {
		coinShare, err := coins[i].LocalShare()
		require.NoError(test, err)
		for j := range n {
			err = coins[j].AddShare(coinShare)
			require.NoError(test, err)
		}
	}

	coinVals := []CoinToss{}
	for _, coin := range coins {
		coinVal, err := coin.Toss()
		require.NoError(test, err)
		coinVals = append(coinVals, coinVal)
	}

	// Verify all coin values are the same and either CoinZero or CoinOne
	require.NotEmpty(test, coinVals)
	first := coinVals[0]
	for i, val := range coinVals {
		require.Equal(test, first, val, "coin value at index %d differs", i)
		require.True(test, val == CoinZero || val == CoinOne, "coin value at index %d is not 0 or 1", i)
	}
}

func TestCommonCoin_TwoValues(test *testing.T) {
	// Idea: simulate a toss of 2 common coins
	//  - output of coin 1 should deterministically be 1
	//  - output of coin 2 should deterministically be 0

	// First create global random generator of G
	// Find such x_0_1 that first bit of hash(gTilde^x_0_1) == 1
	// Find such x_0_2 that first bit of hash(gTilde^x_0_2) == 0
	// Then for both x_0 do

	//  n participants (coins), threshold k (k shares to reconstruct)
	// 	1. Simulate dealing phase:
	// 		sample n x_i private shares and g^x_i verification keys
	// 		based on random generator of G and given secret x_0

	// 	2. create n common coins
	// 	3. each coin does LocalShare (generates a coin share dependant on the coin text and secret key)
	// 	   each coin verifies all proofs of all other coins: require no errors
	// 	   each coin does a coin Toss (reconstruct the gTilde^x_0 and take first bit of its hash):
	// 			require all values to be equal 0 or 1 (depends on secret)

	n := 20
	k := 7

	// 1
	suite := bn254.NewSuiteG1()
	// randomGGenerator is the global VK sampled by the dealer
	randomGGenerator := suite.Point().Pick(random.New())
	CoinMsgText := "HelloCoin"

	hashable, ok := suite.Point().(kyber.HashablePoint)
	if !ok {
		panic("ecdh coin: group point needs to implement hashablePoint")
	}

	gTilde := hashable.Hash([]byte(CoinMsgText))

	x0_1, err := findScalarWithFirstBit(suite, gTilde, CoinOne)
	require.NoError(test, err)
	x0_2, err := findScalarWithFirstBit(suite, gTilde, CoinZero)
	require.NoError(test, err)

	for i, secret := range []kyber.Scalar{x0_2, x0_1} {
		priShares, pubShares := samplePolynomials(suite, k, n, secret, randomGGenerator)

		coinNodes, err := createCoins(
			suite,
			CoinMsgText,
			randomGGenerator,
			priShares,
			pubShares,
			k, n)

		require.NoError(test, err)

		err = distributeShares(coinNodes)
		require.NoError(test, err)

		coinVals, err := tossCoins(coinNodes)
		require.NoError(test, err)
		require.NotEmpty(test, coinVals)

		var expectedVal CoinToss
		if i == 0 {
			expectedVal = CoinZero
		} else {
			expectedVal = CoinOne
		}

		// Verify all coin values are the same and are equal to expected
		for i, val := range coinVals {
			require.Equal(test, expectedVal, val, "coin value at index %d differs", i)
		}
	}
}

func TestCommonCoin_Fair(test *testing.T) {
	// Create a common coin of threshold k distributed among n nodes
	// and toss it for numIter rounds.
	// Check that output equals 0 and 1 at least expectedCoinValThreshold
	// times for each.
	//  - 100 tosses, 30 heads/tails threshold, prob of < 30 heads/tails is < 10^-6, 7.5 seconds
	//  - 50  tosses, 15 heads/tails threshold, prob of < 15 heads/tails is ~ 10^-3, 3.8 seconds

	// 1
	n := 10
	k := 3
	numIters := 50
	expectedCoinValThreshold := 15

	suite := bn254.NewSuiteG1()
	// randomGGenerator is the global VK sampled by the dealer
	randomGGenerator := suite.Point().Pick(random.New())
	CoinMsgText := "HelloCoin"

	secret := suite.Scalar().Pick(suite.RandomStream())

	priShares, pubShares := samplePolynomials(suite, k, n, secret, randomGGenerator)

	coinNodes, err := createCoins(
		suite,
		CoinMsgText,
		randomGGenerator,
		priShares,
		pubShares,
		k, n)

	require.NoError(test, err)

	coinRoundVals := []CoinToss{}
	for i := range numIters {
		err = distributeShares(coinNodes)
		require.NoError(test, err)

		coinVals, err := tossCoins(coinNodes)
		require.NoError(test, err)
		require.NotEmpty(test, coinVals)

		// Verify all coin values are the same and either CoinZero or CoinOne
		first := coinVals[0]
		for i, val := range coinVals {
			require.Equal(test, first, val, "coin value at index %d differs", i)
			require.True(test, val == CoinZero || val == CoinOne, "coin value at index %d is not 0 or 1", i)
		}

		coinRoundVals = append(coinRoundVals, first)

		err = nextCoinsRound(coinNodes, CoinMsgText, i)
		require.NoError(test, err)
	}

	numOnes := 0
	numZeros := 0
	for _, cVal := range coinRoundVals {
		if cVal == CoinOne {
			numOnes++
		} else if cVal == CoinZero {
			numZeros++
		}
	}

	test.Logf("num ones: %d", numOnes)
	test.Logf("num zeros: %d", numZeros)

	require.True(test, numZeros >= expectedCoinValThreshold)
	require.True(test, numOnes >= expectedCoinValThreshold)
}
