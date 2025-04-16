package reedsolomon

import (
	"fmt"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
)

type RSEncoder interface {
	// Encode receives a message of k symbols and encodes it into a code of n symbols
	Encode(msg []kyber.Scalar, n int) ([]*share.PriShare, error)
}

type RSDecoder interface {
	// Decode takes a message of m symbols and tries to decode the original k symbols message
	Decode(msg []*share.PriShare, k int) ([]kyber.Scalar, error)
}

type RSCodes struct {
	kyber.Group
}

func NewRSCodes(group kyber.Group) *RSCodes {
	return &RSCodes{group}
}

func (rs *RSCodes) Encode(msg []kyber.Scalar, n int) ([]*share.PriShare, error) {
	// TODO do we need to check that the length of the message is smaller than n?
	// Create a polynomial from the symbols
	poly := share.CoefficientsToPriPoly(rs.Group, msg)

	// Evaluate the polynomial at n points
	encoded := make([]*share.PriShare, n)
	for i := 0; i < n; i++ {
		enc := poly.Eval(uint32(i))
		encoded[i] = enc
	}
	return encoded, nil
}

func (rs *RSCodes) Decode(msg []*share.PriShare, k int) ([]kyber.Scalar, error) {
	// TODO asserts no erasure or missing values, fix this

	n := len(msg)
	if n < k {
		return nil, fmt.Errorf("not enough scalars to decode")
	}

	// Interpolate using the k first points and retrieve the original polynomial's weights
	poly, err := share.RecoverPriPoly(rs.Group, msg, k, n)
	if err != nil {
		return nil, err
	}

	return poly.Coefficients(), nil
}
