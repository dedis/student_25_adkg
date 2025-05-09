package reedsolomon

import (
	"github.com/HACKERALERT/infectious"
)

// BWCodes Berlekamp-Welch implementation of RS codes
// from this library: https://pkg.go.dev/github.com/HACKERALERT/infectious
type BWCodes struct {
	RSCodes
	fec *infectious.FEC
}

func NewBWCodes(k, n int) *BWCodes {
	fec, err := infectious.NewFEC(k, n)
	if err != nil {
		panic(err)
	}
	return &BWCodes{
		fec: fec,
	}
}

func (rs *BWCodes) Encode(msg []byte) ([]Encoding, error) {
	shares := make([]Encoding, rs.fec.Total())
	output := func(s infectious.Share) {
		sCopy := s.DeepCopy()
		shares[s.Number] = Encoding{
			Idx: int64(sCopy.Number),
			Val: sCopy.Data,
		}
	}

	err := rs.fec.Encode(msg, output)
	return shares, err
}

func (rs *BWCodes) Decode(msg []Encoding) ([]byte, error) {
	shares := encodingsToShares(msg)
	res, err := rs.fec.Decode(nil, shares)
	return res, err
}

func encodingsToShares(encodings []Encoding) []infectious.Share {
	shares := make([]infectious.Share, len(encodings))
	for i, encoding := range encodings {
		shares[i] = infectious.Share{
			Number: int(encoding.Idx),
			Data:   encoding.Val,
		}
	}
	return shares
}
