package marshalling

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
	"go.dedis.ch/protobuf"
)

func TestShareMarshalling_Test(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	sh := &share.PubShare{
		I: uint32(0),
		V: g.Point().Null(),
	}

	bs, err := protobuf.Encode(sh)
	require.NoError(t, err)

	shDecoded := &share.PubShare{}
	err = protobuf.Decode(bs, shDecoded)
	require.NoError(t, err)

	require.True(t, sh.V.Equal(shDecoded.V))
	require.Equal(t, sh.I, shDecoded.I)
}

func TestShareMarshalling_MarshallingPubShare(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	sh := &share.PubShare{
		I: uint32(0),
		V: g.Point().Null(),
	}

	shareBytes, err := MarshalPubShare(sh)
	require.NoError(t, err)

	unmarshalled, err := UnmarshalPubShare(shareBytes, g)
	require.NoError(t, err)

	require.Equal(t, sh.I, unmarshalled.I)
	require.True(t, sh.V.Equal(unmarshalled.V))
}

func TestShareMarshalling_MarshallingPubShares(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	threshold := 3
	nbNodes := threshold*3 + 1
	s := g.Scalar().Pick(g.RandomStream())

	p := share.NewPriPoly(g, threshold, s, random.New())
	pHat := p.Commit(nil)

	shares := pHat.Shares(nbNodes)

	shareBytes, err := MarshalPubShares(shares)
	require.NoError(t, err)

	unmarshalled, err := UnmarshalPubShares(shareBytes, g)
	require.NoError(t, err)

	require.Equal(t, len(shares), len(unmarshalled))
	for i := 0; i < nbNodes; i++ {
		require.Equal(t, shares[i].I, unmarshalled[i].I)
		require.True(t, shares[i].V.Equal(unmarshalled[i].V))
	}
}

func TestShareMarshalling_MarshallingPriShare(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	sh := &share.PriShare{
		I: uint32(0),
		V: g.Scalar().One(),
	}

	shareBytes, err := MarshalPriShare(sh)
	require.NoError(t, err)

	unmarshalled, err := UnmarshalPriShare(shareBytes, g)
	require.NoError(t, err)

	require.Equal(t, sh.I, unmarshalled.I)
	require.True(t, sh.V.Equal(unmarshalled.V))
}
