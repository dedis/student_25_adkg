package pedersencommitment

import (
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
)

// PedPolyCommit creates a polynomial commitment for the given kyber.Scalar.
func PedPolyCommit(s0 kyber.Scalar, t, n int,
	g kyber.Group, g0, g1 kyber.Point) (commit []kyber.Point, sShare, rShare []*share.PriShare, err error) {
	p := share.NewPriPoly(g, t, s0, random.New())
	phi := share.NewPriPoly(g, t, nil, random.New())

	// Compute g0^p(x)
	pCommit := p.Commit(g0)

	// Compute g1^phi(x)
	pHatCommit := phi.Commit(g1)
	// Compute g0^p(x)g1^phi(x)
	publicPoly, err := pCommit.Add(pHatCommit)
	if err != nil {
		return nil, nil, nil, err
	}

	_, commit = publicPoly.Info()
	s := p.Shares(n)
	r := phi.Shares(n)

	return commit, s, r, nil
}

// PedPolyVerify verifies that the given share opens the given polynomial commitment
func PedPolyVerify(commits []kyber.Point, idx int64, si, ri *share.PriShare, g kyber.Group, g0, g1 kyber.Point) bool {

	// Compute PI_0^t v^i^j
	xi := g.Scalar().SetInt64(1 + int64(idx))
	v := g.Point().Null()
	for j := len(commits) - 1; j >= 0; j-- {
		v.Mul(xi, v)
		v.Add(v, commits[j])
	}

	g0si := g.Point().Mul(si.V, g0)
	g1ri := g.Point().Mul(ri.V, g1)
	t := g.Point().Add(g0si, g1ri)

	return v.Equal(t)
}
