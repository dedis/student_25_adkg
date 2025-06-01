package secretsharing

import (
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/protobuf"
)

// MarshalCommitment marshal the given Pedersen commitment to an array
// of bytes. Return an error if marshalling kyber.Point caused an error
func MarshalCommitment(commits []kyber.Point) ([]byte, error) {
	encoded := make([]byte, 0)

	for _, point := range commits {
		bs, err := point.MarshalBinary()
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, bs...)
	}
	return encoded, nil
}

// unmarshalCommitment unmarshal a Pedersen commitment encoded with MarshalCommitment.
// Uses kyber.Group to generate point. The group should be the same as the one used to generate the commitment.
// Returns an error if unmarshalling kyber.Point caused a problem.
func UnmarshalCommitment(bs []byte, g kyber.Group) ([]kyber.Point, error) {
	pointSize := g.Point().MarshalSize()

	//
	shares := make([]kyber.Point, 0)
	start := 0
	for start <= len(bs)-pointSize {
		shareBytes := bs[start : start+pointSize]
		s := g.Point().Null()
		err := s.UnmarshalBinary(shareBytes)
		if err != nil {
			return nil, err
		}
		shares = append(shares, s)

		start = start + pointSize
	}

	return shares, nil
}

func MarshalShares(sShare, rShare *share.PriShare) (sBytes, rBytes []byte, err error) {
	sBytes, err = protobuf.Encode(sShare)
	if err != nil {
		return nil, nil, err
	}
	rBytes, err = protobuf.Encode(rShare)
	return sBytes, rBytes, err
}

func UnmarshalShares(sBytes, rBytes []byte) (sShare, rShare *share.PriShare, err error) {
	sShare = &share.PriShare{}
	err = protobuf.Decode(sBytes, sShare)
	if err != nil {
		return nil, nil, err
	}
	rShare = &share.PriShare{}
	err = protobuf.Decode(rBytes, rShare)
	return sShare, rShare, err
}
