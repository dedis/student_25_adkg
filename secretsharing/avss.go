package secretsharing

import (
	"context"
	"crypto/sha256"
	"errors"
	"student_25_adkg/logging"
	"student_25_adkg/rbc"
	"student_25_adkg/rbc/fourrounds"
	"student_25_adkg/reedsolomon"
	"student_25_adkg/secretsharing/typedefs"

	"github.com/rs/zerolog"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
	"go.dedis.ch/protobuf"
	"golang.org/x/xerrors"
	"google.golang.org/protobuf/proto"
)

var Uint32Size = 4

type SecretShare interface {
	Share(context.Context, kyber.Scalar) error
	Listen(context.Context) (kyber.Scalar, error)
}

type Config struct {
	g  kyber.Group
	g0 kyber.Point
	g1 kyber.Point
	t  int
	n  int
}

type AVSS struct {
	nodeID       int64
	conf         Config
	iface        rbc.AuthenticatedMessageStream
	logger       zerolog.Logger
	shareChannel chan *Deal
}

func NewAVSS(conf Config, nodeID int64, stream rbc.AuthenticatedMessageStream) *AVSS {
	registerPointAndScalarProtobufInterfaces(conf.g)
	return &AVSS{
		conf:         conf,
		logger:       logging.GetLogger(nodeID),
		shareChannel: make(chan *Deal),
		nodeID:       nodeID,
		iface:        stream,
	}
}

// registerPointAndScalarProtobufInterfaces registers the kyber.Point and kyber.Scalar interfaces
// into protobuf so that they can be encoded and decoded. Needs to be called only once
func registerPointAndScalarProtobufInterfaces(g kyber.Group) {
	protobuf.RegisterInterface(func() interface{} {
		return g.Point()
	})
	protobuf.RegisterInterface(func() interface{} {
		return g.Scalar()
	})
}

func PedPolyCommit(p *share.PriPoly, t int, conf Config) (*share.PubPoly, []*share.PriShare, []*share.PriShare) {
	phi := share.NewPriPoly(conf.g, t, nil, random.New())

	// Compute g0^p(x)
	pCommit := p.Commit(conf.g0)
	// Compute g1^phi(x)
	pHatCommit := phi.Commit(conf.g1)

	// Compute g0^p(x)g1^phi(x)
	commit, err := pCommit.Add(pHatCommit)
	if err != nil {
		return nil, nil, nil
	}

	// commit = v

	s := p.Shares(conf.n)
	r := phi.Shares(conf.n)

	return commit, s, r
}

func PedPolyVerify(commitment *share.PubPoly, idx int64, si, ri *share.PriShare, conf Config) bool {
	_, coefficients := commitment.Info()

	// Compute PI_0^t v^i^j
	idxS := conf.g.Scalar().SetInt64(idx)
	pi := conf.g.Point().Null()
	factor := conf.g.Scalar().One()
	for i := 0; i < len(coefficients); i++ {
		c := coefficients[i]
		m := c.Mul(factor, c)
		pi = pi.Add(pi, m)

		factor = factor.Mul(factor, idxS)
	}

	g0si := conf.g0.Mul(si.V, conf.g0)
	g1ri := conf.g1.Mul(ri.V, conf.g1)
	t := g0si.Add(g0si, g1ri)

	return pi.Equal(t)
}

type Deal struct {
	idx int64
	si  *share.PriShare
	ri  *share.PriShare
}

func dealToShareInstruction(deal *Deal) (*typedefs.Instruction, error) {
	siBytes, err := protobuf.Encode(deal.si)
	if err != nil {
		return nil, err
	}
	riBytes, err := protobuf.Encode(deal.ri)
	if err != nil {
		return nil, err
	}

	inst := createShareMessage(siBytes, riBytes, deal.idx)

	return inst, nil
}

func shareMessageToDeal(shareMsg *typedefs.Message_Share) (*Deal, error) {
	si := &share.PriShare{}
	err := protobuf.Decode(shareMsg.GetSi(), si)
	if err != nil {
		return nil, err
	}
	ri := &share.PriShare{}
	err = protobuf.Decode(shareMsg.GetRi(), ri)
	if err != nil {
		return nil, err
	}

	return &Deal{
		idx: shareMsg.Idx,
		si:  si,
		ri:  ri,
	}, nil
}

func decodePubShares(bs []byte, g kyber.Group) ([]*share.PubShare, error) {
	pointSize := g.Point().MarshalSize()
	shareSize := pointSize + Uint32Size

	//
	shares := make([]*share.PubShare, 0)
	start := 0
	for start <= len(bs)-shareSize {
		shareBytes := bs[start : start+shareSize]
		s := &share.PubShare{}
		err := protobuf.Decode(shareBytes, s)
		if err != nil {
			return nil, err
		}
		shares = append(shares, s)

		start = start + shareSize
	}

	return shares, nil
}

func (a *AVSS) predicate(bs []byte) bool {

	shares, err := decodePubShares(bs, a.conf.g)
	if err != nil {
		return false
	}

	// Wait for the share to be received
	s := <-a.shareChannel

	p, err := share.RecoverPubPoly(a.conf.g, shares, a.conf.t, a.conf.n)
	if err != nil {
		return false
	}

	ok := PedPolyVerify(p, s.idx, s.si, s.ri, a.conf)

	return ok
}

func (a *AVSS) sendShares(sShares, rShares []*share.PriShare) error {
	// Broadcast the shares for each
	for i := 1; i <= a.conf.n; i++ {
		d := &Deal{
			si: sShares[i],
			ri: rShares[i],
		}
		inst, err := dealToShareInstruction(d)
		if err != nil {
			return err
		}

		err = a.broadcastInstruction(inst)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *AVSS) Share(ctx context.Context, s kyber.Scalar) error {
	// Randomly sample a polynomial s.t. the origin is at s
	p := share.NewPriPoly(a.conf.g, a.conf.t, s, random.New())
	commit, sShares, rShares := PedPolyCommit(p, a.conf.t, a.conf)

	err := a.sendShares(sShares, rShares)
	if err != nil {
		return err
	}

	rs := reedsolomon.NewBWCodes(a.conf.t, a.conf.n)
	fourRoundRBC := fourrounds.NewFourRoundRBC(a.predicate, sha256.New(), a.conf.t, a.iface, rs, 2, a.nodeID)

	commitBytes, err := protobuf.Encode(commit.Shares(a.conf.n))
	if err != nil {
		return err
	}

	err = fourRoundRBC.RBroadcast(ctx, commitBytes)
	if err != nil {
		return err
	}

	// Sharing finishes when the broadcast finishes

	// TODO reconstruction phase

	return nil
}

func (a *AVSS) Listen(ctx context.Context) (kyber.Scalar, error) {
	rs := reedsolomon.NewBWCodes(a.conf.t, a.conf.n)
	fourRoundsRBC := fourrounds.NewFourRoundRBC(a.predicate, sha256.New(), a.conf.t, a.iface, rs, 2, a.nodeID)

	// Wait for an RBC instance to finish
	err := fourRoundsRBC.Listen(ctx)
	if err != nil {
		return nil, err
	}

	// TODO reconstruction phase

	return nil, nil
}

// start listens for packets on the interface and handles them. Returns a channel that will
// return nil when the protocol finishes or an error if it stopped or any other reason
func (a *AVSS) start(ctx context.Context) chan error {
	finishedChan := make(chan error)
	go func() {
		for {
			bs, err := a.iface.Receive(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					a.logger.Warn().Err(err).Msg("context canceled")
					finishedChan <- err
					return
				}
				a.logger.Error().Err(err).Msg("error receiving message")
				continue
			}
			msg := &typedefs.Instruction{}
			err = protobuf.Decode(bs, msg)
			if err != nil {
				a.logger.Error().Err(err).Msg("error decoding message")
				continue
			}
			_, err = a.handleMsg(msg)
			if err != nil {
				a.logger.Err(err).Msg("Error handling message")
				continue
			}
		}
	}()
	return finishedChan
}

func (a *AVSS) handleMsg(msg *typedefs.Instruction) (bool, error) {
	var err error
	switch msg.GetOp().(type) {
	case *typedefs.Instruction_ShareInst:
		err = a.receiveShare(msg.GetShareInst())
	case *typedefs.Instruction_ReconstructInst:
		// TODO
	default:
		err = xerrors.New("unknown instruction received in AVSS")
	}
	return false, err
}

func (a *AVSS) receiveShare(shareMsg *typedefs.Message_Share) error {
	// Check that the msg is for us
	if shareMsg.GetIdx() != a.nodeID {
		return nil
	}

	deal, err := shareMessageToDeal(shareMsg)
	if err != nil {
		return err
	}
	// Start a thread that will block until the predicate is called to read it
	go func() {
		a.shareChannel <- deal
	}()
	return nil
}

func createShareMessage(si, ri []byte, i int64) *typedefs.Instruction {
	echoMsg := &typedefs.Message_Share{
		Si:  si,
		Ri:  ri,
		Idx: i,
	}
	op := &typedefs.Instruction_ShareInst{ShareInst: echoMsg}
	inst := &typedefs.Instruction{Op: op}
	return inst
}

func (a *AVSS) broadcastInstruction(instruction *typedefs.Instruction) error {
	out, err := proto.Marshal(instruction)
	if err != nil {
		return err
	}

	return a.iface.Broadcast(out)
}
