package fourRounds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
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

// Marshaller represents an interface for an object that can marshal
// and unmarshal some value type
type Marshaller[M any] interface {
	Marshal(M) ([]byte, error)
	Unmarshal([]byte) (M, error)
}

type FourRoundRBC[M any] struct {
	iface      rbc.AuthenticatedMessageStream
	marshaller Marshaller[M]
	pred       func([]M) bool
	hash.Hash
	rs        *reedsolomon.RSCodes
	stopChan  chan struct{}
	threshold int
	sentReady bool
	sync.RWMutex
	echoCount   map[string]int
	readyCounts map[string]int
	readyMis    map[string]map[string]struct{}
	th          []*share.PriShare
	kyber.Group
	r          int
	nbNodes    int
	finalValue []M
	finished   bool
	log        zerolog.Logger
	id         uint32
}

func NewFourRoundRBC[M any](pred func([]M) bool, h hash.Hash, threshold int,
	iface rbc.AuthenticatedMessageStream,
	marshaller Marshaller[M], group kyber.Group, r, nbNodes int, id uint32) *FourRoundRBC[M] {

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

	return &FourRoundRBC[M]{
		iface:       iface,
		marshaller:  marshaller,
		pred:        pred,
		Hash:        h,
		rs:          reedsolomon.NewRSCodes(group),
		stopChan:    make(chan struct{}),
		threshold:   threshold,
		sentReady:   false,
		RWMutex:     sync.RWMutex{},
		echoCount:   make(map[string]int),
		readyCounts: make(map[string]int),
		readyMis:    make(map[string]map[string]struct{}),
		th:          make([]*share.PriShare, 0),
		Group:       group,
		r:           r,
		nbNodes:     nbNodes,
		finalValue:  nil,
		finished:    false,
		log:         logger,
		id:          id,
	}
}

func (f *FourRoundRBC[M]) FreshHash(ms []M) ([]byte, error) {
	f.Hash.Reset()
	for _, m := range ms {
		b, err := f.marshaller.Marshal(m)
		if err != nil {
			return nil, err
		}
		_, err = f.Hash.Write(b)
		if err != nil {
			return nil, err
		}
	}
	h := f.Hash.Sum(nil)
	return h, nil
}

func (f *FourRoundRBC[M]) broadcast(ms []M) error {
	msBytes := make([][]byte, len(ms))
	for i, m := range ms {
		b, err := f.marshaller.Marshal(m)
		if err != nil {
			return err
		}
		msBytes[i] = b
	}
	inst := createProposeMessage(msBytes)
	err := f.broadcastInstruction(inst)
	return err
}

func (f *FourRoundRBC[M]) start(cancelFunc context.CancelFunc) {
	go func() {
		for {
			bs, err := f.iface.Receive(f.stopChan)
			if err != nil {
				// Check if the error is that the receive was stop via the channel or not
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

func (f *FourRoundRBC[M]) RBroadcast(m []M) error {
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

func (f *FourRoundRBC[M]) Listen() error {
	ctx, cancel := context.WithCancel(context.Background())
	f.start(cancel)

	<-ctx.Done()
	return nil
}

func (f *FourRoundRBC[M]) Stop() error {
	f.stopChan <- struct{}{}
	return nil
}

func (f *FourRoundRBC[M]) handleMessage(instruction *typedefs.Instruction) (error, bool) {
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
		finished = f.receiveReady(op.ReadyInst)
	default:
		return xerrors.New("Invalid operation"), false
	}

	return err, finished
}

func (f *FourRoundRBC[M]) receivePropose(m *typedefs.Message_Propose) error {

	valBytes := m.Content
	vals := make([]M, len(valBytes))
	for i, b := range valBytes {
		value, err := f.marshaller.Unmarshal(b)
		if err != nil {
			return err
		}
		vals[i] = value
	}
	if !f.pred(vals) {
		return xerrors.New("Given value did not pass the predicate")
	}

	// Hash the value
	h, err := f.FreshHash(vals)
	if err != nil {
		return err
	}

	scalars := make([]kyber.Scalar, len(m.Content))
	for i, b := range m.Content {
		scalar := f.Group.Scalar().One()
		err := scalar.UnmarshalBinary(b)
		if err != nil {
			return err
		}
		scalars[i] = scalar
	}

	// Encode to have a share to send for each node
	encodings, err := f.rs.Encode(scalars, f.nbNodes)

	// Broadcast an echo message for each encoding
	for _, Mi := range encodings {
		if Mi == nil {
			return err
		}
		MiBytes, err := Mi.V.MarshalBinary()
		if err != nil {
			return err
		}
		echoInst := createEchoMessage(MiBytes, h, Mi.I)
		err = f.broadcastInstruction(echoInst)
		if err != nil {
			return err
		}
	}
	f.log.Info().Msgf("Sent %d echo messages", len(encodings))
	return nil
}

func (f *FourRoundRBC[M]) receiveEcho(msg *typedefs.Message_Echo) error {
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

func (f *FourRoundRBC[M]) receiveReady(msg *typedefs.Message_Ready) bool {
	f.Lock()
	defer f.Unlock()

	// Update the count of ready message received for that hash and check if a READY message needs to be sent
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
			return false
		}
		f.sentReady = true
		f.log.Printf("Sent Ready message for %x", msg.Mi)
	}

	var value []M = nil
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
	// If this value is seen for the first time, add it to T_h and try to reconstruct
	if firstTime {
		MiScalar := f.Scalar()
		err := MiScalar.UnmarshalBinary(msg.Mi)
		if err != nil {
			return false
		}
		f.log.Printf("Received first ready message for %x", msg.Mi)
		f.th = append(f.th, &share.PriShare{
			I: msg.I,
			V: MiScalar,
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
		return true
	}

	return false
}

func (f *FourRoundRBC[M]) reconstruct(expHash []byte) ([]M, bool, error) {
	for ri := 0; ri < f.r; ri++ {
		if len(f.th) < 2*f.threshold+ri+1 {
			// If it is not the case now, it won't be in the next iteration since r increases
			return nil, false, nil
		}
		coefficients, err := f.rs.Decode(f.th, f.threshold+1)
		if err != nil {
			return nil, false, err
		}
		coefficientsMarshalled := make([]M, len(coefficients))
		for i, c := range coefficients {
			b, err := c.MarshalBinary()
			if err != nil {
				return nil, false, err
			}
			m, err := f.marshaller.Unmarshal(b)
			if err != nil {
				return nil, false, err
			}
			coefficientsMarshalled[i] = m
		}
		h, err := f.FreshHash(coefficientsMarshalled)
		if err != nil {
			return nil, false, err
		}

		if bytes.Equal(h, expHash) {
			return coefficientsMarshalled, true, nil

		}
	}
	return nil, false, nil
}

func (f *FourRoundRBC[M]) broadcastInstruction(instruction *typedefs.Instruction) error {
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

func createProposeMessage(ms [][]byte) *typedefs.Instruction {
	proposeMsg := &typedefs.Message_Propose{
		Content: ms,
	}
	msg := &typedefs.Message{Op: &typedefs.Message_ProposeInst{ProposeInst: proposeMsg}}
	inst := &typedefs.Instruction{Operation: msg}
	return inst
}

func (f *FourRoundRBC[M]) checkEchoThreshold(count int, hashReady bool) bool {
	if hashReady {
		return count >= (f.threshold + 1)
	}
	return count >= (2*f.threshold + 1)
}

func (f *FourRoundRBC[M]) checkReadyThreshold(count int) bool {
	return count >= (f.threshold + 1)
}
