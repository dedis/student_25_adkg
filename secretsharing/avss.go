package secretsharing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"student_25_adkg/logging"
	"student_25_adkg/pedersencommitment"
	"student_25_adkg/rbc"
	"student_25_adkg/rbc/fourrounds"
	"student_25_adkg/typedefs"

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
	nodeID        int64
	conf          Config
	iface         rbc.AuthenticatedMessageStream
	rbc           rbc.RBC[[]byte]
	logger        zerolog.Logger
	shareChannel  chan *Deal
	myDeal        *Deal
	shares        []*share.PriShare
	commitment    []kyber.Point
	result        kyber.Scalar
	finishChannel chan struct{}
}

func NewAVSS(conf Config, nodeID int64, stream rbc.AuthenticatedMessageStream, rbc *fourrounds.FourRoundRBC) *AVSS {
	registerPointAndScalarProtobufInterfaces(conf.g)
	return &AVSS{
		conf:          conf,
		logger:        logging.GetLogger(nodeID),
		shareChannel:  make(chan *Deal),
		nodeID:        nodeID,
		iface:         stream,
		rbc:           rbc,
		shares:        make([]*share.PriShare, 0),
		finishChannel: make(chan struct{}),
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

type Deal struct {
	si *share.PriShare
	ri *share.PriShare
}

func shareMessageToDeal(siBytes, riBytes []byte, idx int64) (*Deal, error) {
	si := &share.PriShare{}
	err := protobuf.Decode(siBytes, si)
	if err != nil {
		return nil, err
	}
	ri := &share.PriShare{}
	err = protobuf.Decode(riBytes, ri)
	if err != nil {
		return nil, err
	}

	return &Deal{
		si: si,
		ri: ri,
	}, nil
}

func encodeCommitment(commits []kyber.Point) ([]byte, error) {
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

func decodeCommitment(bs []byte, g kyber.Group) ([]kyber.Point, error) {
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

func (a *AVSS) predicate(bs []byte) bool {

	commitment, err := decodeCommitment(bs, a.conf.g)
	if err != nil {
		return false
	}

	// Wait for the share to be received
	s := <-a.shareChannel

	if a.nodeID == 1 {
		a.logger.Trace().Msgf("Received commitment for node %d", a.nodeID)
	}

	ok := pedersencommitment.PedPolyVerify(commitment, int64(s.si.I), s.si, s.ri, a.conf.g, a.conf.g0, a.conf.g1)
	if ok {
		a.commitment = commitment
	}

	return ok
}

func (a *AVSS) sendShares(sShares, rShares []*share.PriShare) error {
	sSharesBytes := make([][]byte, len(sShares))
	rSharesBytes := make([][]byte, len(rShares))
	indices := make([]int64, len(sShares))
	// Broadcast the shares
	for i := 0; i < a.conf.n; i++ {
		sBytes, err := protobuf.Encode(sShares[i])
		if err != nil {
			return err
		}
		rBytes, err := protobuf.Encode(rShares[i])
		if err != nil {
			return err
		}
		sSharesBytes[i] = sBytes
		rSharesBytes[i] = rBytes
		indices[i] = int64(sShares[i].I)
	}

	shareMessage := createShareMessage(sSharesBytes, rSharesBytes, indices)

	return a.broadcastMessage(shareMessage)
}

func (a *AVSS) Start(ctx context.Context) error {
	go func() {
		a.rbc.Listen(ctx)
	}()

	go func() {
		a.start(ctx)
	}()
	return nil
}

func (a *AVSS) Share(ctx context.Context, s kyber.Scalar) error {
	// Randomly sample a polynomial s.t. the origin is at s
	p := share.NewPriPoly(a.conf.g, a.conf.t, s, random.New())
	commit, sShares, rShares := pedersencommitment.PedPolyCommit(p, a.conf.t, a.conf.n, a.conf.g, a.conf.g0, a.conf.g1)

	err := a.sendShares(sShares, rShares)
	if err != nil {
		return err
	}

	err = a.reliableBroadcastCommitment(ctx, commit)
	if err != nil {
		return err
	}

	siBytes, err := protobuf.Encode(a.myDeal.si)
	if err != nil {
		return err
	}
	riBytes, err := protobuf.Encode(a.myDeal.ri)
	if err != nil {
		return err
	}

	reconstructMessage := createReconstructMessage(siBytes, riBytes, int64(a.myDeal.si.I))
	err = a.broadcastMessage(reconstructMessage)
	if err != nil {
		return err
	}

	return nil
}

func (a *AVSS) waitForRBC(ctx context.Context, commitmentHash []byte) rbc.Instance[[]byte] {
	for {
		select {
		case <-ctx.Done():
			return nil
		case instance := <-a.rbc.GetFinishedChannel():
			if bytes.Equal(instance.Identifier(), commitmentHash) {
				return instance
			}
		}
	}
}

func (a *AVSS) reliableBroadcastCommitment(ctx context.Context, commitment []kyber.Point) error {
	commitBytes, err := encodeCommitment(commitment)
	if err != nil {
		return err
	}

	hash := sha256.New()
	hash.Write(commitBytes)
	commitmentHash := hash.Sum(nil)

	done := make(chan bool)
	go func() {
		a.logger.Info().Msg("Waiting for reliable commitment")
		state := a.waitForRBC(ctx, commitmentHash)

		done <- state != nil
	}()

	err = a.rbc.RBroadcast(commitBytes)
	if err != nil {
		return err
	}

	a.logger.Info().Msg("Reliable commitment sent")

	success := <-done

	if !success {
		return errors.New("failed to broadcast commitment")
	}
	return nil
}

// start listens for packets on the interface and handles them. Returns a channel that will
// return nil when the protocol finishes or an error if it stopped or any other reason
func (a *AVSS) start(ctx context.Context) error {
	for {
		bs, err := a.iface.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				a.logger.Warn().Err(err).Msg("context canceled")
				return err
			}
			a.logger.Error().Err(err).Msg("error receiving message")
			continue
		}
		msg := &typedefs.Instruction{}
		err = proto.Unmarshal(bs, msg)
		if err != nil {
			a.logger.Error().Err(err).Msg("error decoding message")
			continue
		}

		ssMessage, ok := msg.GetOp().(*typedefs.Instruction_SsMessageInst)
		if !ok {
			continue
		}

		_, err = a.handleMsg(ssMessage.SsMessageInst)
		if err != nil {
			a.logger.Err(err).Msg("Error handling message")
			continue
		}

	}
}

func (a *AVSS) handleMsg(msg *typedefs.SSMessage) (bool, error) {
	var err error
	switch msg.GetOp().(type) {
	case *typedefs.SSMessage_ShareInst:
		err = a.receiveShare(msg.GetShareInst())
	case *typedefs.SSMessage_ReconstructInst:
		err = a.receiveReconstruct(msg.GetReconstructInst())
	default:
		err = xerrors.New("unknown instruction received in AVSS")
	}
	return false, err
}

func (a *AVSS) receiveShare(shareMsg *typedefs.SSMessage_Share) error {
	// Extract the shares for this node
	si := shareMsg.GetSi()[a.nodeID-1]
	ri := shareMsg.GetRi()[a.nodeID-1]
	idx := shareMsg.GetIndices()[a.nodeID-1]

	deal, err := shareMessageToDeal(si, ri, idx)
	if err != nil {
		return err
	}
	// Start a thread that will block until the predicate is called to read it
	go func() {
		a.shareChannel <- deal
	}()
	a.myDeal = deal
	return nil
}

func (a *AVSS) receiveReconstruct(receiveMessage *typedefs.SSMessage_Reconstruct) error {
	si := receiveMessage.GetSi()
	ri := receiveMessage.GetRi()
	idx := receiveMessage.GetIdx()

	deal, err := shareMessageToDeal(si, ri, idx)
	if err != nil {
		return err
	}

	if a.commitment == nil || !pedersencommitment.PedPolyVerify(a.commitment, int64(deal.si.I), deal.si, deal.ri, a.conf.g, a.conf.g0, a.conf.g1) {
		return errors.New("no commitment or could not verify share")
	}

	a.shares = append(a.shares, deal.si)
	if len(a.shares) < a.conf.t+1 {
		return nil
	}

	// Enough shares to reconstruct the original value
	if a.result == nil {
		a.result = a.reconstruct()
		close(a.finishChannel)
	}
	return nil
}

func (a *AVSS) GetFinishedChannel() <-chan struct{} {
	return a.finishChannel
}

func (a *AVSS) reconstruct() kyber.Scalar {
	scalars := make([]kyber.Scalar, len(a.shares))
	for i, s := range a.shares {
		scalars[i] = s.V
	}
	poly := share.CoefficientsToPriPoly(a.conf.g, scalars)
	return poly.Secret()
}

func createShareMessage(si, ri [][]byte, indices []int64) *typedefs.SSMessage {
	shareMessage := &typedefs.SSMessage_Share{
		Si:      si,
		Ri:      ri,
		Indices: indices,
	}
	message := &typedefs.SSMessage{Op: &typedefs.SSMessage_ShareInst{ShareInst: shareMessage}}
	return message
}

func createReconstructMessage(si, ri []byte, idx int64) *typedefs.SSMessage {
	reconstructMessage := &typedefs.SSMessage_Reconstruct{
		Si:  si,
		Ri:  ri,
		Idx: idx,
	}
	message := &typedefs.SSMessage{Op: &typedefs.SSMessage_ReconstructInst{ReconstructInst: reconstructMessage}}
	return message
}

func (a *AVSS) broadcastMessage(message *typedefs.SSMessage) error {
	avssInstruction := &typedefs.Instruction_SsMessageInst{SsMessageInst: message}
	instruction := &typedefs.Instruction{Op: avssInstruction}
	out, err := proto.Marshal(instruction)
	if err != nil {
		return err
	}

	return a.iface.Broadcast(out)
}
