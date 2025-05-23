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
	rs              reedsolomon.RSCodes
	threshold       int
	states          map[string]*State
	finishedChannel chan *State
	log             zerolog.Logger
	nodeID          int64
	sync.RWMutex
}

func NewFourRoundRBC(predicate func([]byte) bool, h hash.Hash, threshold int,
	iface rbc.AuthenticatedMessageStream,
	rs reedsolomon.RSCodes, nodeID int64) *FourRoundRBC {

	return &FourRoundRBC{
		iface:           iface,
		predicate:       predicate,
		Hash:            h,
		rs:              rs,
		threshold:       threshold,
		RWMutex:         sync.RWMutex{},
		states:          make(map[string]*State),
		finishedChannel: make(chan *State, 100),
		log:             logging.GetLogger(nodeID),
		nodeID:          nodeID,
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

func (f *FourRoundRBC) RBroadcast(message []byte) error {
	// Send the broadcast
	inst := createProposeMessage(message)
	err := f.broadcastInstruction(inst)
	return err
}

// Listen starts listening to incoming messages from the network and handles
// them. This method blocks until the context passed is either cancelled
// (context.Canceled) or its deadline exceeded (context.DeadlineExceeded)
func (f *FourRoundRBC) Listen(ctx context.Context) error {
	for {
		bs, err := f.iface.Receive(ctx)
		if err != nil {
			// Stop looping if the context was stopped
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				// Cancel all running instances
				f.stopInstances()
				close(f.finishedChannel)
				return err
			}
			f.log.Err(err).Msg("Error receiving message")

		}

		msg := &typedefs.Instruction{}
		err = proto.Unmarshal(bs, msg)
		if err != nil {
			f.log.Err(err).Msg("Error unmarshalling")
			continue
		}

		err = f.handleMessage(msg)
		if err != nil {
			f.log.Err(err).Msg("Error handling message")
			continue
		}
	}
}

func (f *FourRoundRBC) stopInstances() {
	for _, state := range f.states {
		state.SetFailedIfNotFinished()
	}
}

func (f *FourRoundRBC) handleMessage(instruction *typedefs.Instruction) error {
	var err error
	switch op := instruction.GetOperation().GetOp().(type) {
	case *typedefs.Message_ProposeInst:
		err = f.receivePropose(op.ProposeInst)
	case *typedefs.Message_EchoInst:
		err = f.receiveEcho(op.EchoInst)
	case *typedefs.Message_ReadyInst:
		err = f.receiveReady(op.ReadyInst)
	default:
		err = xerrors.New("Invalid operation")
	}

	return err
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

func (f *FourRoundRBC) getOrCreateState(messageHash []byte) *State {
	f.Lock()
	defer f.Unlock()
	if state, ok := f.states[string(messageHash)]; ok {
		return state
	}
	state := NewState(messageHash)
	f.states[string(messageHash)] = state
	return state
}

func (f *FourRoundRBC) receiveEcho(msg *typedefs.Message_Echo) error {
	state := f.getOrCreateState(msg.GetMessageHash())

	echoCount := state.IncrementEchoCount()

	// If the hash has received enough READY messages, then the threshold only needs be t+1
	readyCount := state.ReadyCount()

	readyThreshold := f.checkReadyThreshold(readyCount)
	echoThreshold := f.checkEchoThreshold(echoCount, readyThreshold)
	sendReady := echoThreshold && !state.SentReady()

	if !sendReady {
		return nil
	}

	nodeShare := msg.GetEncodingShares()[f.nodeID-1]
	nodeShareIndex := msg.GetSharesIndices()[f.nodeID-1]

	// Atomically set sent ready if sending worked
	var err error
	state.SetSentReadyOnSuccess(func() bool {
		// Send the ready message and set sentReady
		inst := createReadyMessage(nodeShare, msg.GetMessageHash(), nodeShareIndex)
		err = f.broadcastInstruction(inst)
		return err == nil
	})

	return err
}

func (f *FourRoundRBC) receiveReady(msg *typedefs.Message_Ready) error {
	state := f.getOrCreateState(msg.GetMessageHash())

	readyCount := state.IncrementReadyCount()
	echoCount := state.EchoCount()

	readyThreshold := f.checkReadyThreshold(readyCount)
	sendReady := !state.SentReady() && readyThreshold && f.checkEchoThreshold(echoCount, true)

	if sendReady {
		var err error
		// Atomically try to send the ready message and set sentReady
		state.SetSentReadyOnSuccess(func() bool {
			inst := createReadyMessage(msg.GetEncodingShare(), msg.GetMessageHash(), msg.GetIndex())
			err = f.broadcastInstruction(inst)
			if err != nil {
				f.log.Err(err).Msg("Failed to broadcast ready message")
				return false
			}
			return true
		})
		if err != nil {
			return err
		}
	}

	share := &reedsolomon.Encoding{
		Val: msg.GetEncodingShare(),
		Idx: msg.GetIndex(),
	}

	_ = state.AddReadyShareIfAbsent(share)

	// Try to reconstruct
	shares := state.ReadyShares()
	value, finished, err := f.reconstruct(shares, msg.GetMessageHash())
	if err != nil {
		f.log.Err(err).Msg("failed to reconstruct")
	}
	if finished {
		state.SetFinalValue(value)
		f.finishedChannel <- state
	}

	return nil
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

func (f *FourRoundRBC) GetFinalValue(messageHash []byte) []byte {
	f.RLock()
	defer f.RUnlock()
	state, ok := f.states[string(messageHash)]
	if !ok {
		return nil
	}
	return state.FinalValue()
}

func (f *FourRoundRBC) GetState(messageHash []byte) (*State, bool) {
	f.RLock()
	defer f.RUnlock()
	state, ok := f.states[string(messageHash)]
	return state, ok
}

func (f *FourRoundRBC) GetFinishChannel() <-chan *State {
	return f.finishedChannel
}
