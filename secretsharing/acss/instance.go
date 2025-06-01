package acss

import (
	"sync"

	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
)

type Instance struct {
	hashIdentifier     []byte
	rbcFinished        bool
	commit             []kyber.Point
	sShare             *share.PriShare
	rShare             *share.PriShare
	sShares            []*share.PriShare
	reconstructed      bool
	reconstructedValue kyber.Scalar
	sync.RWMutex
}

func NewInstance(hash []byte) *Instance {
	return &Instance{hashIdentifier: hash}
}

func (inst *Instance) Identifier() []byte {
	return inst.hashIdentifier
}

func (inst *Instance) RBCFinished() bool {
	inst.RLock()
	defer inst.RUnlock()
	return inst.rbcFinished
}

func (inst *Instance) Commit() []kyber.Point {
	inst.RLock()
	defer inst.RUnlock()
	return inst.commit
}

func (inst *Instance) SShare() *share.PriShare {
	inst.RLock()
	defer inst.RUnlock()
	return inst.sShare
}

func (inst *Instance) RShare() *share.PriShare {
	inst.RLock()
	defer inst.RUnlock()
	return inst.rShare
}

func (inst *Instance) SShares() []*share.PriShare {
	inst.RLock()
	defer inst.RUnlock()
	duplicate := make([]*share.PriShare, len(inst.sShares))
	for i, sShare := range inst.sShares {
		duplicate[i] = &share.PriShare{
			V: sShare.V,
			I: sShare.I,
		}
	}
	return duplicate
}

func (inst *Instance) Reconstructed() bool {
	inst.RLock()
	defer inst.RUnlock()
	return inst.reconstructed
}

func (inst *Instance) ReconstructedValue() kyber.Scalar {
	inst.RLock()
	defer inst.RUnlock()
	return inst.reconstructedValue
}

func (inst *Instance) SetRBCResult(commit []kyber.Point, sShare, rShare *share.PriShare) {
	inst.Lock()
	defer inst.Unlock()
	inst.rbcFinished = true
	inst.commit = commit
	inst.sShare = sShare
	inst.rShare = rShare
}

func (inst *Instance) AppendSShare(sShare *share.PriShare) int {
	inst.Lock()
	defer inst.Unlock()
	inst.sShares = append(inst.sShares, sShare)
	return len(inst.sShares)
}

func (inst *Instance) SetReconstructed(scalar kyber.Scalar) {
	inst.Lock()
	defer inst.Unlock()
	inst.reconstructed = true
	inst.reconstructedValue = scalar
}
