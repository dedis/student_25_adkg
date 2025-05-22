package fourrounds

import (
	"bytes"
	"context"
	"errors"
	"hash"
	"student_25_adkg/logging"
	"student_25_adkg/rbc"
	"student_25_adkg/rbc/fourrounds/typedefs"
	"student_25_adkg/reedsolomon"
	"sync"

	"github.com/rs/zerolog"
	"golang.org/x/xerrors"
	"google.golang.org/protobuf/proto"
)

type FourRoundRBC struct {
	iface     rbc.AuthenticatedMessageStream
	predicate func([]byte) bool
	hash.Hash
	rs        reedsolomon.RSCodes
	threshold int
	sentReady bool
	sync.RWMutex
	echoCount   map[string]int
	readyCounts map[string]int
	readyShares map[string]map[*reedsolomon.Encoding]struct{}
	r           int
	finalValue  []byte
	finished    bool
	log         zerolog.Logger
	nodeID      int64
}

func NewFourRoundRBC(predicate func([]byte) bool, h hash.Hash, threshold int,
	iface rbc.AuthenticatedMessageStream,
	rs reedsolomon.RSCodes, r int, nodeID int64) *FourRoundRBC {

	return &FourRoundRBC{
		iface:       iface,
		predicate:   predicate,
		Hash:        h,
		rs:          rs,
		threshold:   threshold,
		sentReady:   false,
		RWMutex:     sync.RWMutex{},
		echoCount:   make(map[string]int),
		readyCounts: make(map[string]int),
		readyShares: make(map[string]map[*reedsolomon.Encoding]struct{}),
		r:           r,
		finalValue:  nil,
		finished:    false,
		log:         logging.GetLogger(nodeID),
		nodeID:      nodeID,
	}
}

func (f *FourRoundRBC) FreshHash(bs []byte) ([]byte, error) {
	f.Hash.Reset()

	// Write the bytes
	_, err := f.Hash.Write(bs)
	if err != nil {
		return nil, err
	}
	h := f.Hash.Sum(nil)
	return h, nil
}

func (f *FourRoundRBC) broadcast(bs []byte) error {
	inst := createProposeMessage(bs)
	err := f.broadcastInstruction(inst)
	return err
}

// start listens for packet on the interface and handles them. Returns a channel that will
// return nil when the protocol finishes or an error if it stopped or any other reason.
// The go routine only stops when the channel is canceled
func (f *FourRoundRBC) start(ctx context.Context) chan error {
	returnChan := make(chan error)
	go func() {
		for {
			bs, err := f.iface.Receive(ctx)
			if err != nil {
				// Stop looping if the context was stopped
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					returnChan <- err
					return
				}
				f.log.Err(err).Msg("Error receiving message")
				continue
			}

			msg := &typedefs.Instruction{}
			err = proto.Unmarshal(bs, msg)
			if err != nil {
				f.log.Err(err).Msg("Error unmarshalling")
				continue
			}
			finished, err := f.handleMessage(msg)
			if err != nil {
				f.log.Err(err).Msg("Error handling message")
				continue
			}
			if finished {
				returnChan <- err
				return
			}
		}
	}()
	return returnChan
}

func (f *FourRoundRBC) RBroadcast(ctx context.Context, m []byte) error {
	finishedChan := f.start(ctx)

	// Send the broadcast
	err := f.broadcast(m)
	if err != nil {
		return err
	}

	err = <-finishedChan
	return err
}

func (f *FourRoundRBC) Listen(ctx context.Context) error {
	finishedChan := f.start(ctx)

	err := <-finishedChan
	return err
}

func (f *FourRoundRBC) handleMessage(instruction *typedefs.Instruction) (bool, error) {
	var err error
	finished := false
	switch op := instruction.GetOperation().GetOp().(type) {
	case *typedefs.Message_ProposeInst:
		err = f.receivePropose(op.ProposeInst)
	case *typedefs.Message_EchoInst:
		err = f.receiveEcho(op.EchoInst)
	case *typedefs.Message_ReadyInst:
		finished, err = f.receiveReady(op.ReadyInst)
	default:
		err = xerrors.New("Invalid operation")
	}

	return finished, err
}

func (f *FourRoundRBC) receivePropose(msg *typedefs.Message_Propose) error {
	if !f.predicate(msg.GetContent()) {
		return rbc.ErrPredicateRejected
	}

	// Hash the value
	h, err := f.FreshHash(msg.GetContent())
	if err != nil {
		return err
	}

	// Encode to have a share to send for each node
	encodings, err := f.rs.Encode(msg.GetContent())
	if err != nil {
		return err
	}

	// Broadcast an echo message for each encoding
	echoInst := createEchoMessage(encodings, h)
	err = f.broadcastInstruction(echoInst)
	if err != nil {
		return err
	}
	return nil
}

func (f *FourRoundRBC) receiveEcho(msg *typedefs.Message_Echo) error {
	f.Lock()
	defer f.Unlock()

	count, ok := f.echoCount[string(msg.GetMessageHash())]
	if !ok {
		count = 0
	}
	count++
	f.echoCount[string(msg.GetMessageHash())] = count

	// If the hash has received enough READY messages, then the threshold only needs be t+1
	readyCount, ok := f.readyCounts[string(msg.GetMessageHash())]
	if !ok {
		readyCount = 0
	}
	readyThreshold := f.checkReadyThreshold(readyCount)
	echoThreshold := f.checkEchoThreshold(count, readyThreshold)
	sendReady := echoThreshold && !f.sentReady

	if !sendReady {
		return nil
	}

	nodeShare := msg.GetEncodingShares()[f.nodeID-1]
	nodeShareIndex := msg.GetSharesIndices()[f.nodeID-1]

	// Send the ready message and set sentReady
	inst := createReadyMessage(nodeShare, msg.GetMessageHash(), nodeShareIndex)
	err := f.broadcastInstruction(inst)
	if err != nil {
		return err
	}
	f.sentReady = true
	return nil
}

func (f *FourRoundRBC) receiveReady(msg *typedefs.Message_Ready) (bool, error) {
	f.Lock()
	defer f.Unlock()

	count, ok := f.readyCounts[string(msg.GetMessageHash())]
	if !ok {
		count = 0
	}
	count++
	f.readyCounts[string(msg.GetMessageHash())] = count

	echoes, ok := f.echoCount[string(msg.GetMessageHash())]
	readyThreshold := f.checkReadyThreshold(count)
	sendReady := ok && !f.sentReady && readyThreshold && f.checkEchoThreshold(echoes, true)

	if sendReady {
		// Send the ready message and set sentReady
		inst := createReadyMessage(msg.GetEncodingShare(), msg.GetMessageHash(), msg.GetIndex())
		err := f.broadcastInstruction(inst)
		if err != nil {
			f.log.Err(err).Msg("Failed to broadcast ready message")
			return false, err
		}
		f.sentReady = true
	}

	share := &reedsolomon.Encoding{
		Val: msg.GetEncodingShare(),
		Idx: msg.GetIndex(),
	}

	_ = f.addReadyShareIfAbsent(share, msg.GetMessageHash())

	// Try to reconstruct
	shares := f.getSharesForMessage(msg.GetMessageHash())
	value, finished, err := f.reconstruct(shares, msg.GetMessageHash())
	if err != nil {
		f.log.Err(err).Msg("failed to reconstruct")
	}
	if finished {
		f.finalValue = value
		f.finished = true
	}

	return finished, nil
}

func (f *FourRoundRBC) getSharesForMessage(messageHash []byte) []*reedsolomon.Encoding {
	messageShares, ok := f.readyShares[string(messageHash)]
	if !ok {
		return nil
	}
	shares := make([]*reedsolomon.Encoding, 0, len(messageShares))
	for messageShare := range messageShares {
		shares = append(shares, messageShare)
	}
	return shares
}

// addReadyShareIfAbsent chek if the given share of the corresponding
// message hash has already been received. If not, save it. Return
// true if it was absent and added to the map. False otherwise
func (f *FourRoundRBC) addReadyShareIfAbsent(share *reedsolomon.Encoding, messageHash []byte) bool {
	messageReadyShares, ok := f.readyShares[string(messageHash)]

	if !ok {
		messageReadyShares = make(map[*reedsolomon.Encoding]struct{})
	}
	// Check if the share is already in the map
	_, ok = messageReadyShares[share]

	// Put the value in the map (if it was already in, nothing will happen)
	messageReadyShares[share] = struct{}{}
	// Update the map
	f.readyShares[string(messageHash)] = messageReadyShares

	return !ok
}

func (f *FourRoundRBC) reconstruct(shares []*reedsolomon.Encoding, expHash []byte) ([]byte, bool, error) {
	if len(shares) < 2*f.threshold+1 {
		return nil, false, nil
	}
	coefficients, err := f.rs.Decode(shares)
	if err != nil {
		return nil, false, err
	}

	h, err := f.FreshHash(coefficients)
	if err != nil {
		return nil, false, err
	}

	if bytes.Equal(h, expHash) {
		return coefficients, true, nil
	}
	return nil, false, nil
}

func (f *FourRoundRBC) broadcastInstruction(instruction *typedefs.Instruction) error {
	out, err := proto.Marshal(instruction)
	if err != nil {
		return err
	}

	return f.iface.Broadcast(out)
}

func createReadyMessage(mi, h []byte, i int64) *typedefs.Instruction {
	readyMsg := &typedefs.Message_Ready{
		EncodingShare: mi,
		MessageHash:   h,
		Index:         i,
	}
	msg := &typedefs.Message{Op: &typedefs.Message_ReadyInst{ReadyInst: readyMsg}}
	inst := &typedefs.Instruction{Operation: msg}
	return inst
}

func createEchoMessage(shares []*reedsolomon.Encoding, h []byte) *typedefs.Instruction {
	sharesBytes := make([][]byte, len(shares))
	sharesIndices := make([]int64, len(shares))
	for i, s := range shares {
		sharesIndices[i] = s.Idx
		sharesBytes[i] = s.Val
	}

	echoMsg := &typedefs.Message_Echo{
		EncodingShares: sharesBytes,
		MessageHash:    h,
		SharesIndices:  sharesIndices,
	}
	msg := &typedefs.Message{Op: &typedefs.Message_EchoInst{EchoInst: echoMsg}}
	inst := &typedefs.Instruction{Operation: msg}
	return inst
}

func createProposeMessage(ms []byte) *typedefs.Instruction {
	proposeMsg := &typedefs.Message_Propose{
		Content: ms,
	}
	msg := &typedefs.Message{Op: &typedefs.Message_ProposeInst{ProposeInst: proposeMsg}}
	inst := &typedefs.Instruction{Operation: msg}
	return inst
}

func (f *FourRoundRBC) checkEchoThreshold(count int, hashReady bool) bool {
	if hashReady {
		return count >= (f.threshold + 1)
	}
	return count >= (2*f.threshold + 1)
}

func (f *FourRoundRBC) checkReadyThreshold(count int) bool {
	return count >= (f.threshold + 1)
}

func (f *FourRoundRBC) GetFinalValue() []byte {
	f.RLock()
	defer f.RUnlock()
	return f.finalValue
}
