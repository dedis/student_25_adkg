package secretsharing

import (
	"context"

	"go.dedis.ch/kyber/v4"
)

type SecretShare interface {
	Share(scalar kyber.Scalar) error
	Start(context.Context)
}
