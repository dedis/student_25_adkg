package fourRounds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"golang.org/x/xerrors"
	"google.golang.org/protobuf/proto"
	"hash"
	"os"
	"strconv"
	"student_25_adkg/rbc"
	"student_25_adkg/rbc/fourRounds/typedefs"
	"student_25_adkg/reedsolomon"
	"sync"
	"time"
)

// Define the logger
var (
	logout = zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		// Format the node ID
		FormatPrepare: func(e map[string]interface{}) error {
			e["id"] = fmt.Sprintf("[%s]", e["id"])
			return nil
		},
		// Change the order in which things appear
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.LevelFieldName,
			"id",
			zerolog.MessageFieldName,
		},
		// Prevent the id from being printed again
		FieldsExclude: []string{"id"},
	}
)

type FourRoundRBC struct {
	iface rbc.AuthenticatedMessageStream
	pred  func([]byte) bool
	hash.Hash
	rs        reedsolomon.RSCodes
	stopChan  chan struct{}
	threshold int
	sentReady bool
	sync.RWMutex
	echoCount   map[string]int
	readyCounts map[string]int
	readyMis    map[string]map[string]struct{}
	th          []reedsolomon.Encoding
	r           int
	finalValue  []byte
	finished    bool
	log         zerolog.Logger
	id          uint32
}

func NewFourRoundRBC(pred func([]byte) bool, h hash.Hash, threshold int,
	iface rbc.AuthenticatedMessageStream,
	rs reedsolomon.RSCodes, r int, id uint32) *FourRoundRBC {

	// Disable logging based on the GLOG environment variable
	var logLevel zerolog.Level
	if os.Getenv("GLOG") == "no" {
		logLevel = zerolog.Disabled
	} else {
		logLevel = zerolog.InfoLevel
	}

	logger := zerolog.New(logout).
		Level(logLevel).
		With().
		Timestamp().
		Str("id", strconv.Itoa(int(id))).
		Logger()

	return &FourRoundRBC{
		iface:       iface,
		pred:        pred,
		Hash:        h,
		rs:          rs,
		stopChan:    make(chan struct{}),
		threshold:   threshold,
		sentReady:   false,
		RWMutex:     sync.RWMutex{},
		echoCount:   make(map[string]int),
		readyCounts: make(map[string]int),
		readyMis:    make(map[string]map[string]struct{}),
		th:          make([]reedsolomon.Encoding, 0),
		r:           r,
		finalValue:  nil,
		finished:    false,
		log:         logger,
		id:          id,
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

func (f *FourRoundRBC) start(cancelFunc context.CancelFunc) {
	go func() {
		for {
			bs, err := f.iface.Receive(f.stopChan)
			if err != nil {
				// Check if the error is that the "receive" was stop via the channel or not
				if errors.Is(err, rbc.NodeStoppedError{}) {
					// The channel was stopped so we just return
					cancelFunc()
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
			err, finished := f.handleMessage(msg)
			if err != nil {
				f.log.Err(err).Msg("Error handling message")
				continue
			}
			if finished {
				f.log.Err(err).Msg("Protocol terminated")
				cancelFunc()
				return
			}
		}
	}()
}

func (f *FourRoundRBC) RBroadcast(m []byte) error {
	ctx, cancel := context.WithCancel(context.Background())
	f.start(cancel)

	// Send the broadcast
	err := f.broadcast(m)
	if err != nil {
		cancel()
		return err
	}

	<-ctx.Done()
	return nil
}

func (f *FourRoundRBC) Listen() error {
	ctx, cancel := context.WithCancel(context.Background())
	f.start(cancel)

	<-ctx.Done()
	return nil
}

func (f *FourRoundRBC) Stop() error {
	f.stopChan <- struct{}{}
	return nil
}

func (f *FourRoundRBC) handleMessage(instruction *typedefs.Instruction) (error, bool) {
	var err error = nil
	finished := false
	switch op := instruction.GetOperation().GetOp().(type) {
	case *typedefs.Message_ProposeInst:
		f.log.Info().Msg("Received propose message")
		err = f.receivePropose(op.ProposeInst)
	case *typedefs.Message_EchoInst:
		f.log.Info().Msgf("Received echo message with id %d and msg: %x", op.EchoInst.I, op.EchoInst.Mi)
		err = f.receiveEcho(op.EchoInst)
	case *typedefs.Message_ReadyInst:
		f.log.Info().Msg("Received ready message")
		finished, err = f.receiveReady(op.ReadyInst)
	default:
		err = xerrors.New("Invalid operation")
	}

	return err, finished
}

func (f *FourRoundRBC) receivePropose(m *typedefs.Message_Propose) error {
	if !f.pred(m.Content) {
		return xerrors.New("Given value did not pass the predicate")
	}

	// Hash the value
	h, err := f.FreshHash(m.Content)
	if err != nil {
		return err
	}

	// Encode to have a share to send for each node
	encodings, err := f.rs.Encode(m.Content)

	// Broadcast an echo message for each encoding
	for _, Mi := range encodings {
		echoInst := createEchoMessage(Mi.Val, h, uint32(Mi.Idx))
		err = f.broadcastInstruction(echoInst)
		if err != nil {
			return err
		}
	}
	f.log.Info().Msgf("Sent %d echo messages", len(encodings))
	return nil
}

func (f *FourRoundRBC) receiveEcho(msg *typedefs.Message_Echo) error {
	f.Lock()
	defer f.Unlock()

	// Ignore ECHO message that are not for our index
	if msg.I != f.id {
		return nil
	}

	// Count this message and check if we need to send a READY message
	count, ok := f.echoCount[string(msg.H)]
	// Update the count
	if !ok {
		count = 0
	}
	count += 1

	// Update the count
	f.echoCount[string(msg.H)] = count

	// If the hash has received enough READY messages, then the threshold only needs be t+1
	readyCount, ok := f.readyCounts[string(msg.H)]
	if !ok {
		readyCount = 0
	}
	hashReady := f.checkReadyThreshold(readyCount)

	// Check if we received enough (taking into account if we already received enough ready messages for that hash)
	sendReady := f.checkEchoThreshold(count, hashReady) && !f.sentReady

	if !sendReady {
		return nil
	}

	// Send the ready message and set sentReady
	inst := createReadyMessage(msg.Mi, msg.H, msg.I)
	err := f.broadcastInstruction(inst)
	if err != nil {
		return err
	}
	f.sentReady = true
	f.log.Printf("Sent Ready message for %x", msg.Mi)
	return nil
}

func (f *FourRoundRBC) receiveReady(msg *typedefs.Message_Ready) (bool, error) {
	f.Lock()
	defer f.Unlock()

	// Update the count of ready messages received for that hash and check if a READY message needs to be sent
	count, ok := f.readyCounts[string(msg.H)]
	if !ok {
		count = 0
	}
	count += 1
	// Update the count
	f.readyCounts[string(msg.H)] = count

	// If enough READY messages have been received and enough ECHO messages, then send a READY message
	// if not already sent
	echoes, ok := f.echoCount[string(msg.H)]
	sendReady := ok && !f.sentReady && f.checkReadyThreshold(count) && f.checkReadyThreshold(echoes)

	if sendReady {
		// Send the ready message and set sentReady
		inst := createReadyMessage(msg.Mi, msg.H, msg.I)
		err := f.broadcastInstruction(inst)
		if err != nil {
			f.log.Err(err).Msg("Failed to broadcast ready message")
			return false, err
		}
		f.sentReady = true
		f.log.Printf("Sent Ready message for %x", msg.Mi)
	}

	var value []byte = nil
	hashMis, ok := f.readyMis[string(msg.H)]

	if !ok {
		hashMis = make(map[string]struct{})
	}
	_, notFirst := hashMis[string(msg.Mi)]
	firstTime := !notFirst

	// Put the value in the map
	hashMis[string(msg.Mi)] = struct{}{}
	// Update the map
	f.readyMis[string(msg.H)] = hashMis

	finished := false
	var err error
	// If this value is seen for the first time, add it to T_h and try to reconstruct
	if firstTime {
		f.log.Printf("Received first ready message for %x", msg.Mi)
		f.th = append(f.th, reedsolomon.Encoding{
			Val: msg.Mi,
			Idx: int(msg.I),
		})
		f.log.Printf("Got %d messges in th", len(f.th))
		// Try to reconstruct

		value, finished, err = f.reconstruct(msg.H)
		if err != nil {
			f.log.Printf("Failed to reconstruct: %v", err)
		}
	}

	if finished {
		f.finalValue = value
		f.finished = true
		return true, nil
	}

	return false, nil
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

func createReadyMessage(mi, h []byte, i uint32) *typedefs.Instruction {
	readyMsg := &typedefs.Message_Ready{
		Mi: mi,
		H:  h,
		I:  i,
	}
	msg := &typedefs.Message{Op: &typedefs.Message_ReadyInst{ReadyInst: readyMsg}}
	inst := &typedefs.Instruction{Operation: msg}
	return inst
}

func createEchoMessage(mi, h []byte, i uint32) *typedefs.Instruction {
	echoMsg := &typedefs.Message_Echo{
		Mi: mi,
		H:  h,
		I:  i,
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
