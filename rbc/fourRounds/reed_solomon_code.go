package fourRounds

import (
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
)

// RSEnc takes a message of k symbols treating it as a k-1 degree polynomial and outputs m
// evaluations of that polynomial
func RSEnc(message []kyber.Scalar, m int, g kyber.Group) []kyber.Scalar {
	k := len(message)
	evals := make([]kyber.Scalar, m)

	// Compute poly(i) = message[0] * i^0 + ... + message[k]i^k
	for i := 0; i < m; i++ {
		scalarI := g.Scalar().SetInt64(int64(i))
		evals[i] = eval(message, k, scalarI, g)
	}

	return evals
}

func eval(poly []kyber.Scalar, k int, i kyber.Scalar, g kyber.Group) kyber.Scalar {
	val := g.Scalar().Zero()
	factor := g.Scalar().One()
	for j := 0; j < k; j++ {
		coefficient := poly[j].Clone()
		factored := coefficient.Mul(coefficient, factor)
		val = val.Add(val, factored)
		factor = factor.Mul(factor, i)
	}
	return val
}

func RSDec(symbols []kyber.Scalar, k, r int, g kyber.Group) ([]kyber.Scalar, error) {
	// TODO Fix this to take into consideration transmission errors. For now it assumes all value are correct
	shares := make([]*share.PriShare, k)
	for i := 0; i < k; i++ {
		shares[i] = &share.PriShare{
			I: uint32(i),
			V: symbols[i],
		}

	}
	priPoly, err := share.RecoverPriPoly(g, shares, k, k) // TODO I don't think this works as I want it
	if err != nil {
		return nil, err
	}
	scalars := priPoly.Coefficients()
	return scalars, err
}
