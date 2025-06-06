package secretsharing

import (
	"context"

	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
)

type Config struct {
	Group     kyber.Group
	Base0     kyber.Point
	Base1     kyber.Point
	Threshold int
	NbNodes   int
}

type Instance interface {
	Finished() bool
	Reconstructed() bool
	ReconstructedValue() kyber.Scalar
	RBCFinished() bool
	Identifier() []byte
	SShare() *share.PriShare
	RShare() *share.PriShare
	Commit() []kyber.Point
}

type SecretShare interface {
	Share(scalar kyber.Scalar) (Instance, error)
	Reconstruct(Instance) error
	Start(context.Context) error
	GetInstances() []Instance
	GetFinishedChannel() <-chan Instance
	GetIndex() int64
}
