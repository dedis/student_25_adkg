package pedersencommitment

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"go.dedis.ch/kyber/v4/share"
)

// TestPedersenCommitment_PolyCommit runs the commitment algorithm and tests
// that the returned arrays are correct
func TestPedersenCommitment_PolyCommit(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	threshold := 3
	n := 3*threshold + 1
	g0 := g.Point().Base()
	g1 := g0.Mul(g.Scalar().Pick(g.RandomStream()), g0)

	// Secret
	secret := g.Scalar().Pick(g.RandomStream())

	// Run commitment
	v, s, r, err := PedPolyCommit(secret, threshold, n, g, g0, g1)
	require.NoError(t, err)

	// Recover the polynomials p and phi
	sPoly, err := share.RecoverPriPoly(g, s, threshold, n)
	require.NoError(t, err)
	sCommit := sPoly.Commit(g0)
	_, sCommits := sCommit.Info()

	rPoly, err := share.RecoverPriPoly(g, r, threshold, n)
	require.NoError(t, err)
	rCommit := rPoly.Commit(g1)
	_, rCommits := rCommit.Info()

	require.Equal(t, len(v), len(sCommits))
	require.Equal(t, len(v), len(rCommits))

	// The array v should be the sum of the commitment of p and phi
	for i := 0; i < threshold; i++ {
		g0ai := sCommits[i]
		g1bi := rCommits[i]

		c := g.Point().Add(g0ai, g1bi)

		require.True(t, c.Equal(v[i]))
	}

}

// TestPedersenCommitment_SimpleEndToEnd runs the commitment algorithm and applies
// the verify method and check that it passes
func TestPedersenCommitment_SimpleEndToEnd(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	threshold := 3
	n := 3*threshold + 1
	g0 := g.Point().Base()
	g1 := g0.Mul(g.Scalar().Pick(g.RandomStream()), g0)

	// Secret
	secret := g.Scalar().Pick(g.RandomStream())

	// Run commitment
	v, s, r, err := PedPolyCommit(secret, threshold, n, g, g0, g1)
	require.NoError(t, err)

	for i := range s {
		si := s[i]
		ri := r[i]
		idx := si.I

		ok := PedPolyVerify(v, int64(idx), si, ri, g, g0, g1)
		require.True(t, ok, "PedPolyVerify at idx %d did not work", idx)
	}
}
