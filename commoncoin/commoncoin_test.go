package commoncoin

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/pairing/bn254"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
)

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
	CoinMsgText := "HelloCoin"
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
