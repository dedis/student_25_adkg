package rbc

import (
	"go.dedis.ch/kyber/v4"
)

type ReedSolomonCode struct {
	tokenize func([]byte) [][]byte
}

func (rsc *ReedSolomonCode) RSEnc(message []byte, m int, g kyber.Group) ([][]byte, error) {
	shards := rsc.tokenize(message)
	coeffs := make([]kyber.Scalar, len(message))
	for i, _ := range coeffs {
		shard := shards[i]
		scalar := g.Scalar()
		err := scalar.UnmarshalBinary(shard)
		if err != nil {
			return nil, err
		}
		coeffs[i] = scalar
	}

	// Evaluate at m points
	evs := make([][]byte, m)
	for i := 0; i < m; i++ {
		iS := g.Scalar().SetInt64(int64(i))
		tot := coeffs[0]
		for j := 1; j < len(coeffs); j++ {
			c := coeffs[j]
			c.Mul(c, iS)
			tot = tot.Add(tot, c)
		}
		evs[i] = tot
	}
	return evs, nil
}
