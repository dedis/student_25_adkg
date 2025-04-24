package secretsharing

import (
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
)

type Config struct {
	g kyber.Group
	t int
	n int
}

type AVSS struct {
	conf Config
}

func NewAVSS() *AVSS {
	return &AVSS{}
}

func PedPolyCommit(p *share.PriPoly, t int, conf Config) (*share.PubPoly, *share.PriPoly) {
	pHat := share.NewPriPoly(conf.g, t, nil, random.New())
	h := conf.g.Point().Pick(random.New())

	pCommit := p.Commit(nil)
	pHatCommit := pHat.Commit(h)

	commit, err := pCommit.Add(pHatCommit)
	if err != nil {
		return nil, nil
	}
	return commit, pHat
}

type Deal struct {
	si *share.PriShare
	ri *share.PriShare
}

func (avss *AVSS) Deal(s kyber.Scalar) ([]Deal, *share.PubPoly) {
	// Randomly sample a polynomial s.t. the origin is a t s
	p := share.NewPriPoly(avss.conf.g, avss.conf.t, s, random.New())
	commit, pHat := PedPolyCommit(p, avss.conf.t, avss.conf)
	deals := make([]Deal, p.Threshold())
	for i := 1; i <= avss.conf.n; i++ {
		d := Deal{
			si: p.Eval(uint32(i)),
			ri: pHat.Eval(uint32(i)),
		}
		deals[i-1] = d
	}

	return deals, commit
}
