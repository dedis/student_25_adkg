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
	echoCount           map[string]int
	readyCounts         map[string]int
	readyEncodingShares map[string]map[string]struct{}
	th                  []reedsolomon.Encoding
	r                   int
	finalValue          []byte
	finished            bool
	log                 zerolog.Logger
	nodeID              int64
}

func NewFourRoundRBC(predicate func([]byte) bool, h hash.Hash, threshold int,
	iface rbc.AuthenticatedMessageStream,
	rs reedsolomon.RSCodes, r int, nodeID int64) *FourRoundRBC {

	return &FourRoundRBC{
		iface:               iface,
		predicate:           predicate,
		Hash:                h,
		rs:                  rs,
		threshold:           threshold,
		sentReady:           false,
		RWMutex:             sync.RWMutex{},
		echoCount:           make(map[string]int),
		readyCounts:         make(map[string]int),
		readyEncodingShares: make(map[string]map[string]struct{}),
		th:                  make([]reedsolomon.Encoding, 0),
		r:                   r,
		finalValue:          nil,
		finished:            false,
		log:                 logging.GetLogger(nodeID),
		nodeID:              nodeID,
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
// // return nil when the protocol finishes or an error if it stopped or any other reason
func (f *FourRoundRBC) start(ctx context.Context) chan error {
	returnChan := make(chan error)
	go func() {
		for {
			bs, err := f.iface.Receive(ctx)
			if err != nil {
				// Stop looping if the context was stopped
				if errors.Is(err, context.Canceled) {
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
				f.log.Err(err).Msg("Protocol terminated")
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
	for _, Mi := range encodings {
		echoInst := createEchoMessage(Mi.Val, h, Mi.Idx)
		err = f.broadcastInstruction(echoInst)
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *FourRoundRBC) receiveEcho(msg *typedefs.Message_Echo) error {
	f.Lock()
	defer f.Unlock()

	// Ignore ECHO message that are not for our index
	if msg.GetIndex() != f.nodeID {
		return nil
	}

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
	sendReady := f.checkEchoThreshold(count, readyThreshold) && !f.sentReady

	if !sendReady {
		return nil
	}

	// Send the ready message and set sentReady
	inst := createReadyMessage(msg.GetEncodingShare(), msg.GetMessageHash(), msg.GetIndex())
	err := f.broadcastInstruction(inst)
	if err != nil {
		return err
	}
	f.sentReady = true
	f.log.Printf("Sent Ready message for %x", msg.GetEncodingShare())
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
	sendReady := ok && !f.sentReady && f.checkReadyThreshold(count) && f.checkReadyThreshold(echoes)

	if sendReady {
		// Send the ready message and set sentReady
		inst := createReadyMessage(msg.GetEncodingShare(), msg.GetMessageHash(), msg.GetIndex())
		err := f.broadcastInstruction(inst)
		if err != nil {
			f.log.Err(err).Msg("Failed to broadcast ready message")
			return false, err
		}
		f.sentReady = true
		f.log.Printf("Sent Ready message for %x", msg.GetEncodingShare())
	}

	firstTime := f.addReadyEncodingShareIfFirstTimeSeen(string(msg.GetMessageHash()), string(msg.GetEncodingShare()))

	// If this value is seen for the first time, add it to T_h and try to reconstruct
	if firstTime {
		f.log.Printf("Received first ready message for %x", msg.GetEncodingShare())
		f.th = append(f.th, reedsolomon.Encoding{
			Val: msg.GetEncodingShare(),
			Idx: msg.GetIndex(),
		})
		f.log.Printf("Got %d messges in th", len(f.th))
		// Try to reconstruct

		value, finished, err := f.reconstruct(msg.GetMessageHash())
		if err != nil {
			f.log.Printf("Failed to reconstruct: %v", err)
		}
		if finished {
			f.finalValue = value
			f.finished = true
			return true, nil
		}
	}

	return false, nil
}

// addReadyEncodingShareIfFirstTimeSeen chek if the given share of the encoding corresponding
// to the hashed message has already been received. If not, mark as received. Return
// true if it was the first time. False otherwise and nothing happens
func (f *FourRoundRBC) addReadyEncodingShareIfFirstTimeSeen(messageHash, encodingShare string) bool {
	hashEncodingShare, ok := f.readyEncodingShares[messageHash]

	if !ok {
		hashEncodingShare = make(map[string]struct{})
	}
	_, notFirst := hashEncodingShare[encodingShare]

	// Put the value in the map
	hashEncodingShare[encodingShare] = struct{}{}
	// Update the map
	f.readyEncodingShares[messageHash] = hashEncodingShare

	return !notFirst
}

func (f *FourRoundRBC) reconstruct(expHash []byte) ([]byte, bool, error) {
	for ri := 0; ri < f.r; ri++ {
		if len(f.th) < 2*f.threshold+ri+1 {
			// If it is not the case now, it won't be in the next iteration since r increases
			return nil, false, nil
		}

		coefficients, err := f.rs.Decode(f.th)
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

func createEchoMessage(mi, h []byte, i int64) *typedefs.Instruction {
	echoMsg := &typedefs.Message_Echo{
		EncodingShare: mi,
		MessageHash:   h,
		Index:         i,
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
