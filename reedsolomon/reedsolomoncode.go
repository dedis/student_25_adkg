package reedsolomon

import (
	"github.com/HACKERALERT/infectious"
)

type RSEncoder interface {
	// Encode receives a message of k symbols and encodes it into a code of n symbols
	Encode(msg []byte) ([]infectious.Share, error)
}

type RSDecoder interface {
	// Decode takes a message of m symbols and tries to decode the original k symbols message
	Decode(msg []infectious.Share) ([]byte, error)
}

type RSCodes interface {
	RSEncoder
	RSDecoder
}

// BWCodes Berlekamp-Welch implementation of RS codes
// from this library: https://pkg.go.dev/github.com/Picocrypt/infectious#NewFEC
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

func (rs *BWCodes) Encode(msg []byte) ([]infectious.Share, error) {
	shares := make([]infectious.Share, rs.fec.Total())
	output := func(s infectious.Share) {
		shares[s.Number] = s.DeepCopy()
	}

	err := rs.fec.Encode(msg, output)
	return shares, err
}

func (rs *BWCodes) Decode(msg []infectious.Share) ([]byte, error) {
	res, err := rs.fec.Decode(nil, msg)
	return res, err
}
