package main

import (
	"errors"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"go.dedis.ch/kyber/v4/share"
	dkg "go.dedis.ch/kyber/v4/share/dkg/pedersen"
	"go.dedis.ch/kyber/v4/sign/eddsa"
	"go.dedis.ch/kyber/v4/sign/schnorr"
	"go.dedis.ch/kyber/v4/util/random"
	"testing"
)

type TestNode struct {
	Index   uint32
	Private kyber.Scalar
	Public  kyber.Point
	dkg     *dkg.DistKeyGenerator
	res     *dkg.Result
}

func NewTestNode(s dkg.Suite, index int) *TestNode {
	private := s.Scalar().Pick(random.New())
	public := s.Point().Mul(private, nil)
	return &TestNode{
		Index:   uint32(index),
		Private: private,
		Public:  public,
	}
}

func GenerateTestNodes(s dkg.Suite, n int) []*TestNode {
	tns := make([]*TestNode, n)
	for i := 0; i < n; i++ {
		tns[i] = NewTestNode(s, i)
	}
	return tns
}

func NodesFromTest(tns []*TestNode) []dkg.Node {
	nodes := make([]dkg.Node, len(tns))
	for i := 0; i < len(tns); i++ {
		nodes[i] = dkg.Node{
			Index:  tns[i].Index,
			Public: tns[i].Public,
		}
	}
	return nodes
}

// inits the dkg structure
func SetupNodes(nodes []*TestNode, c *dkg.Config) {
	nonce := dkg.GetNonce()
	for _, n := range nodes {
		c2 := *c
		c2.Longterm = n.Private
		c2.Nonce = nonce
		nodeDKG, err := dkg.NewDistKeyHandler(&c2)
		if err != nil {
			panic(err)
		}
		n.dkg = nodeDKG
	}
}

func RunDKG(t *testing.T, tns []*TestNode, conf dkg.Config) ([]*dkg.Result, *share.PubShare) {

	SetupNodes(tns, &conf)
	var deals []*dkg.DealBundle
	for _, node := range tns {
		d, err := node.dkg.Deals()
		require.NoError(t, err)
		deals = append(deals, d)
	}

	var respBundles []*dkg.ResponseBundle
	for _, node := range tns {
		resp, err := node.dkg.ProcessDeals(deals)
		require.NoError(t, err)
		if resp != nil {
			respBundles = append(respBundles, resp)
		}
	}

	var justifs []*dkg.JustificationBundle
	var results []*dkg.Result
	for _, node := range tns {
		res, just, err := node.dkg.ProcessResponses(respBundles)
		if !errors.Is(err, dkg.ErrEvicted) {
			// there should not be any other error than eviction
			require.NoError(t, err)
		}
		if res != nil {
			results = append(results, res)
		} else if just != nil {
			justifs = append(justifs, just)
		}
	}

	// Get the public

	if len(justifs) == 0 {
		// Get the public
		publicPoly := share.NewPubPoly(suite, suite.Point().Base(), results[0].Key.Commits)
		publicKey := publicPoly.Eval(0)
		return results, publicKey
	}

	for _, node := range tns {
		res, err := node.dkg.ProcessJustifications(justifs)
		if errors.Is(err, dkg.ErrEvicted) {
			continue
		}
		require.NoError(t, err)
		require.NotNil(t, res)
		results = append(results, res)
	}

	// Get the public
	publicPoly := share.NewPubPoly(suite, suite.Point().Base(), results[0].Key.Commits)
	publicKey := publicPoly.Eval(0)

	return results, publicKey
}

func Sign(results []*dkg.Result, threshold int, msg []byte, suite dkg.Suite, n int) ([]byte, error) {
	if len(results) < threshold {
		return nil, errors.New("not enough nodes to recover the secret")
	}

	var shares []*share.PriShare
	for _, res := range results {
		shares = append(shares, res.Key.PriShare())
	}

	secretPoly, err := share.RecoverPriPoly(suite, shares, threshold, n)
	if err != nil {
		return nil, err
	}

	secret := secretPoly.Secret()

	publicPoly := share.NewPubPoly(suite, suite.Point().Base(), results[0].Key.Commits)
	publicKey := publicPoly.Eval(0).V

	ed := eddsa.NewEdDSA(suite.RandomStream())
	ed.Secret = secret
	ed.Public = publicKey

	signedMsg, err := ed.Sign(msg)

	return signedMsg, err
}

// Test_run_dkg_and_sign_message runs DKG with 5 nodes and then tries to sign some random message using the result
// and check if the signature can successfully be verified.
func Test_run_dkg_and_sign_message(t *testing.T) {
	// Settings
	n := 5
	thr := 4
	suite := edwards25519.NewBlakeSHA256Ed25519()
	tns := GenerateTestNodes(suite, n)
	list := NodesFromTest(tns)
	conf := dkg.Config{
		Suite:     suite,
		NewNodes:  list,
		Threshold: thr,
		Auth:      schnorr.NewScheme(suite),
	}

	// Run the DKG protocol
	results, publicKey := RunDKG(t, tns, conf)

	// Randomly select a subset of just enough (i.e. threshold) of nodes to try and sign the message
	resultsSubset := make([]*dkg.Result, thr)
	for i := 0; i < thr; i++ {
		resultsSubset[i] = results[i]
	}

	msg := []byte("Hello")
	signed, err := Sign(resultsSubset, thr, msg, suite, n)
	require.NoError(t, err)

	err = eddsa.Verify(publicKey.V, msg, signed)
}
