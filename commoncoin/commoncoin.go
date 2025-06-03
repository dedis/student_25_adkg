package commoncoin

// DH (ECDH) common coin described in section 6 of https://eprint.iacr.org/2000/034.pdf

// The value of a coin C is obtained by first hashing C to obtain ˜g ∈ G,
// then raising ˜g to a secret exponent x0 ∈ Zq to obtain ˜g0 ∈ G, and
// finally hashing ˜g0 to obtain the value F (C) ∈ {0, 1}. (x0 is the secret,
// parties have it's shares)

// In the dealing phase, the dealer selects k coefficients of a random polynomial f (T ) over Zq
// of degree less than k and a random generator g of G. For 0 ≤ i ≤ n, let xi = f (i) and gi = gxi .
// Party Pi’s secret key SKi is xi, and his verification key VKi is gi. The global verification key
// VK consists of a description of G (which includes q) and g.

// Each party Pi holds a share xi  of x0; its share of F (C) is ˜gxi , along with a “validity proof.”
// Shares of coin C can then be combined to obtain ˜g0 by interpolation “in the exponent.”

import (
	"errors"
	"fmt"
	"student_25_adkg/logging"

	"github.com/rs/zerolog"
	kyber "go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/proof/dleq"
	"go.dedis.ch/kyber/v4/share"
)

type Suite interface {
	kyber.Group // with hashable points
	kyber.HashFactory
	kyber.XOFFactory
	kyber.Random
}

// not really used here, more of a draft of an interface
// for the coin which is supposed to work with different groups
type CommonCoin interface {
	LocalShare() (CoinShare, error)
	Toss() (CoinToss, error) // combines shares internally
	VerifyShare() error
	AddShare(CoinShare) error
	NumShares() int
	ResetCoinID(coinID string) error // to start another round
}

type CoinShare struct {
	GTildeShare share.PubShare
	Proof       *dleq.Proof
}

type CoinToss int

const (
	CoinZero CoinToss = iota
	CoinOne
	InvalidTossResult
)

type ECDHCommonCoin struct {
	coinID  string
	s       Suite
	gVK     kyber.Point // globally known VK (random generator of G)
	gTilde  kyber.Point // coinID hashed to curve
	si      *share.PriShare
	shares  map[uint32]*CoinShare
	vks     []*share.PubShare // verifications
	partyID uint32
	k       int // threshold
	n       int // number of parties
	logger  zerolog.Logger
}

// NewECDHCommonCoin creates a new instance of ECDHCommonCoin for a given coinID and cryptographic suite.
//
// Parameters:
//   - coinID:      Unique identifier for the coin instance + round (concatenated text and round, see tests).
//   - suite:       Cryptographic suite: should contain group with hashable points (to hash coinID to curve)
//   - globalVK:    The global verification key (some generator of the group G).
//   - vks:         Public commits for private shares (aka verification shares) for all participants.
//   - si:          The private share for this party.
//   - partyID:     The unique identifier of this party (index > 0, 0 in secret sharing is reserved for the secret).
//   - k:           Threshold number of shares required to reconstruct the coin.
//   - n:           Total number of participants.
//
// Returns:
//   - *ECDHCommonCoin: Pointer to the initialized ECDHCommonCoin instance.
//   - error:           Error if the suite does not support hashable points.
//
// The function hashes the coinID to the curve to derive a unique generator (gTilde) for this coin instance.
// It initializes the coin's state, including its verification keys, private share, and logger.
// This function is typically called by each participant to set up their local coin instance for a protocol round.

func NewECDHCommonCoin(
	coinID string,
	suite Suite,
	globalVK kyber.Point,
	vks []*share.PubShare,
	si *share.PriShare,
	partyID uint32,
	k int,
	n int) (*ECDHCommonCoin, error) {
	hashable, ok := suite.Point().(kyber.HashablePoint)
	if !ok {
		return &ECDHCommonCoin{}, errors.New("ecdh coin: group point needs to implement hashablePoint")
	}

	gTilde := hashable.Hash([]byte(coinID))
	cShares := make(map[uint32]*CoinShare)

	c := &ECDHCommonCoin{
		coinID:  coinID,
		s:       suite,
		gVK:     globalVK,
		si:      si,
		partyID: partyID,
		k:       k,
		n:       n,
		gTilde:  gTilde,
		vks:     vks,
		shares:  cShares,
		logger:  logging.GetLogger(int64(partyID)),
	}

	return c, nil
}

func (c *ECDHCommonCoin) LocalShare() (*CoinShare, error) {
	proof, _, gTildeShare, err := dleq.NewDLEQProof(c.s, c.gVK, c.gTilde, c.si.V)
	// gTildeShare = ~g_i = x_i*~g
	// _ = x_i*gVK = VK_i, already known to all parties

	if err != nil {
		return &CoinShare{}, err
	}

	pubGTildeShare := share.PubShare{V: gTildeShare, I: uint32(c.partyID)}
	coinShare := &CoinShare{GTildeShare: pubGTildeShare, Proof: proof}
	return coinShare, nil
}

// given k different coin shares with valid proofs
// reconstruct gTilde_0 (gTilde^secret) = g̃0 = product over α∈S of (g̃_i)^(λ_{i,0})
// then compute its hash and return its last bit
func (c *ECDHCommonCoin) Toss() (CoinToss, error) {
	validPubShares := []*share.PubShare{}
	tossResult := InvalidTossResult
	for _, coinShare := range c.shares {
		if c.VerifyShare(coinShare) != nil {
			c.logger.Warn().Msgf("failed coin share verification of party %d", coinShare.GTildeShare.I)
			continue
		}
		validPubShares = append(validPubShares, &coinShare.GTildeShare)
	}
	if len(validPubShares) < c.k {
		return tossResult, fmt.Errorf("not enough calid coin shares to reveil common coin value")
	}

	// RecoverPubPoly(...) seems as a bit if an overkill,
	// could copy lagrangeBasis(...) from share instead
	pubPoly, err := share.RecoverPubPoly(c.s, validPubShares, c.k, c.n)
	if err != nil {
		return tossResult, err
	}

	hash := c.s.Hash()
	if _, err := pubPoly.Commit().MarshalTo(hash); err != nil {
		return tossResult, errors.New("err marshalling gTilde^secret to the hash function")
	}

	hashGTildeZero := hash.Sum(nil)
	tossResult = CoinToss((hashGTildeZero[0] >> 7) & 1)

	return tossResult, nil
}

func (c *ECDHCommonCoin) VerifyShare(cShare *CoinShare) error {
	proof := cShare.Proof
	gTildeShare := cShare.GTildeShare.V
	sharePartyID := cShare.GTildeShare.I
	sharePartyVK := c.vks[sharePartyID].V
	if sharePartyID != c.vks[sharePartyID].I {
		return errors.New("coin share is stored at incorrect index")
	}

	return proof.Verify(c.s, c.gVK, c.gTilde, sharePartyVK, gTildeShare)
}

// coinID is defined by entity which calls the coin
// In case of ABA it's the id of ABA + round,
// so for one ABA for each round a new coinID needs to be set,
// gTilde recomputed and shares emptied. Other fields stay the same.
func (c *ECDHCommonCoin) ResetCoinID(coinID string) error {
	hashable, ok := c.s.Point().(kyber.HashablePoint)
	if !ok {
		return errors.New("group point needs to implement HashablePoint")
	}
	c.coinID = coinID
	c.gTilde = hashable.Hash([]byte(coinID))
	for k := range c.shares {
		delete(c.shares, k)
	}

	return nil
}

func (c *ECDHCommonCoin) AddShare(coinShare *CoinShare) error {
	err := c.VerifyShare(coinShare)
	if err != nil {
		return err
	}
	c.shares[coinShare.GTildeShare.I] = coinShare
	return nil
}
