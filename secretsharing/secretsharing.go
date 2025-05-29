package secretsharing

import (
	"context"

	"go.dedis.ch/kyber/v4"
)

type Config struct {
	Group     kyber.Group
	Base0     kyber.Point
	Base1     kyber.Point
	Threshold int
	NbNodes   int
}

type SecretShare interface {
	Share(scalar kyber.Scalar) error
	Reconstruct(ctx context.Context) kyber.Scalar
	Start(context.Context)
}
