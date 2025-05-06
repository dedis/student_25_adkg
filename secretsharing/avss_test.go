package secretsharing

import (
	"go.dedis.ch/kyber/v4/group/edwards25519"
)

var defaultThreshold = 3

func getDefaultConfig() Config {
	g := edwards25519.NewBlakeSHA256Ed25519()
	offset := g.Scalar().Pick(g.RandomStream())
	g0 := g.Point().Base()
	g1 := g0.Mul(offset, g0)

	return Config{
		g:  g,
		g0: g0,
		g1: g1,
		t:  defaultThreshold,
		n:  3*defaultThreshold + 1,
	}
}
