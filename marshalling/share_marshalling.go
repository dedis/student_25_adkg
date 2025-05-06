package marshalling

import (
	"encoding/binary"

	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
)

var Uint32Size = 4

func MarshalPriShare(share *share.PriShare) ([]byte, error) {
	bs := make([]byte, share.V.MarshalSize()+Uint32Size)
	scalarMarshalled, err := share.V.MarshalBinary()
	if err != nil {
		return nil, err
	}
	copy(bs[4:], scalarMarshalled)

	idxBytes := make([]byte, Uint32Size)
	binary.BigEndian.PutUint32(idxBytes, share.I)
	copy(bs[:4], idxBytes)
	return bs, nil
}

func MarshalPubShare(share *share.PubShare) ([]byte, error) {

	bs := make([]byte, share.V.MarshalSize()+4)
	pointMarshalled, err := share.V.MarshalBinary()
	if err != nil {
		return nil, err
	}
	copy(bs[Uint32Size:], pointMarshalled)

	idxBytes := make([]byte, Uint32Size)
	binary.BigEndian.PutUint32(idxBytes, share.I)
	copy(bs[:Uint32Size], idxBytes)
	return bs, nil

	/*
		bs, err := protobuf.Encode(share)
		return bs, err

	*/
}

func MarshalPubShares(shares []*share.PubShare) ([]byte, error) {
	bs := make([]byte, 0)
	for _, s := range shares {
		shareMarshalled, err := MarshalPubShare(s)
		if err != nil {
			return nil, err
		}
		bs = append(bs, shareMarshalled...)
	}
	return bs, nil
}

func UnmarshalPriShare(bs []byte, g kyber.Group) (*share.PriShare, error) {
	idx := binary.BigEndian.Uint32(bs[:Uint32Size])

	scalar := g.Scalar().One()
	err := scalar.UnmarshalBinary(bs[Uint32Size:])
	if err != nil {
		return nil, err
	}

	return &share.PriShare{
		I: idx,
		V: scalar,
	}, nil
}

func UnmarshalPubShare(bs []byte, g kyber.Group) (*share.PubShare, error) {

	idx := binary.BigEndian.Uint32(bs[:Uint32Size])

	point := g.Point().Null()
	err := point.UnmarshalBinary(bs[Uint32Size:])
	if err != nil {
		return nil, err
	}

	return &share.PubShare{
		I: idx,
		V: point,
	}, nil

	/*
		s := &share.PubShare{}
		err := protobuf.Decode(bs, s)
		return s, err

	*/
}

func UnmarshalPubShares(data []byte, g kyber.Group) ([]*share.PubShare, error) {
	pointSize := g.Point().MarshalSize()
	shareSize := pointSize + Uint32Size

	//
	shares := make([]*share.PubShare, 0)
	start := 0
	for start <= len(data)-shareSize {
		shareBytes := data[start : start+shareSize]
		s, err := UnmarshalPubShare(shareBytes, g)
		if err != nil {
			return nil, err
		}
		shares = append(shares, s)

		start = start + shareSize
	}

	return shares, nil
}
