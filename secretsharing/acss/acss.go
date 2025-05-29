package acss

import (
	"context"
	"student_25_adkg/networking"

	"go.dedis.ch/kyber/v4"
)

type ACSS struct {
	iface networking.NetworkInterface
}

func NewACSS(iface networking.NetworkInterface) *ACSS {
	return &ACSS{iface: iface}
}

func (a *ACSS) Share(scalar kyber.Scalar) error {
	return nil
}

func (a *ACSS) Start(ctx context.Context) error {
	return nil
}
