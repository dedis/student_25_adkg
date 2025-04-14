package fourRounds

import (
	"bytes"
	"context"
	"fmt"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
	"golang.org/x/xerrors"
	"google.golang.org/protobuf/proto"
	"hash"
	"log"
	"os"
	"student_25_adkg/rbc"
	"student_25_adkg/rbc/fourRounds/typedefs"
	"student_25_adkg/reedsolomon"
	"student_25_adkg/tools"
	"sync"
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
	threshold int
	sentReady bool
	sync.RWMutex
	echoCount   tools.ConcurrentMap[string, int]
	readyCounts tools.ConcurrentMap[string, int]
	readyMis    tools.ConcurrentMap[string, map[string]struct{}]
	th          []*share.PriShare
	kyber.Group
	r          int
	nbNodes    int
	finalValue []M
	finished   bool
	log        *log.Logger
	i          uint32
}

func NewFourRoundRBC[M any](pred func([]M) bool, h hash.Hash, threshold int,
	iface rbc.AuthenticatedMessageStream,
	marshaller Marshaller[M], group kyber.Group, r, nbNodes int, i uint32) *FourRoundRBC[M] {
	return &FourRoundRBC[M]{
		iface:       iface,
		marshaller:  marshaller,
		pred:        pred,
		Hash:        h,
		rs:          reedsolomon.NewRSCodes(group),
		threshold:   threshold,
		sentReady:   false,
		RWMutex:     sync.RWMutex{},
		echoCount:   *tools.NewConcurrentMap[string, int](),
		readyCounts: *tools.NewConcurrentMap[string, int](),
		readyMis:    *tools.NewConcurrentMap[string, map[string]struct{}](),
		th:          make([]*share.PriShare, 0),
		Group:       group,
		r:           r,
		nbNodes:     nbNodes,
		finalValue:  nil,
		finished:    false,
		log:         log.New(os.Stdout, fmt.Sprintf("[%d]", i), log.LstdFlags|log.Lshortfile),
		i:           i,
	}
}

func (frRbc *FourRoundRBC[M]) FreshHash(ms []M) ([]byte, error) {
	frRbc.Hash.Reset()
	for _, m := range ms {
		b, err := frRbc.marshaller.Marshal(m)
		if err != nil {
			return nil, err
		}
		_, err = frRbc.Hash.Write(b)
		if err != nil {
			return nil, err
		}
	}
	h := frRbc.Hash.Sum(nil)
	return h, nil
}

func (frRbc *FourRoundRBC[M]) broadcast(ms []M) error {
	msBytes := make([][]byte, len(ms))
	for i, m := range ms {
		b, err := frRbc.marshaller.Marshal(m)
		if err != nil {
			return err
		}
		msBytes[i] = b
	}
	inst := createProposeMessage(msBytes)
	err := frRbc.broadcastInstruction(inst)
	return err
}

func (frRbc *FourRoundRBC[M]) RBroadcast(m []M) error {
	ctx, cancel := context.WithCancel(context.Background())
	frRbc.iface.AddHandler(func(bytes []byte) error {
		msg := &typedefs.Instruction{}
		err := proto.Unmarshal(bytes, msg)
		if err != nil {
			return err
		}
		err, finished := frRbc.handleMessage(msg)
		if finished { // TODO check if we should make sure err is nil before reading finished (in theory no if we always return false in case of an error)
			cancel()
		}
		return err
	})

	// Send the broadcast
	err := frRbc.broadcast(m)
	if err != nil {
		cancel()
		return err
	}

	<-ctx.Done()

	return nil
}

func (frRbc *FourRoundRBC[M]) Listen() error {
	ctx, cancel := context.WithCancel(context.Background())
	frRbc.iface.AddHandler(func(received []byte) error {
		// TODO make this handler method re-usable as it is exactly the same used in the RBroadcast method
		if received == nil {
			return nil
		}
		msg := &typedefs.Instruction{}
		err := proto.Unmarshal(received, msg)
		if err != nil {
			return err
		}
		if err != nil {
			return err
		}
		err, finished := frRbc.handleMessage(msg)
		if finished {
			cancel()
		}
		return nil
	})

	// Now the handler registered above will take over the control flow, and we just need to wait for the context
	<-ctx.Done()

	return nil
}

func (frRbc *FourRoundRBC[M]) handleMessage(instruction *typedefs.Instruction) (error, bool) {
	var err error = nil
	finished := false
	switch op := instruction.Operation.Op.(type) {
	case *typedefs.Message_ProposeInst:
		frRbc.log.Printf("Received propose message")
		err = frRbc.receivePropose(op.ProposeInst)
	case *typedefs.Message_EchoInst:
		frRbc.log.Printf("Received echo message with i %d and msg: %x", op.EchoInst.I, op.EchoInst.Mi)
		err = frRbc.receiveEcho(op.EchoInst)
	case *typedefs.Message_ReadyInst:
		frRbc.log.Printf("Received ready message")
		finished = frRbc.receiveReady(op.ReadyInst)
	}

	return err, finished
}

func (frRbc *FourRoundRBC[M]) receivePropose(m *typedefs.Message_Propose) error {

	valBytes := m.Content
	vals := make([]M, len(valBytes))
	for i, b := range valBytes {
		value, err := frRbc.marshaller.Unmarshal(b)
		if err != nil {
			return err
		}
		vals[i] = value
	}
	if !frRbc.pred(vals) {
		return xerrors.New("Given value did not pass the predicate")
	}

	// Hash the value
	h, err := frRbc.FreshHash(vals)
	if err != nil {
		return err
	}

	scalars := make([]kyber.Scalar, len(m.Content))
	for i, b := range m.Content {
		scalar := frRbc.Group.Scalar().One()
		err := scalar.UnmarshalBinary(b)
		if err != nil {
			return err
		}
		scalars[i] = scalar
	}

	// Encode to have a share to send for each node
	encodings, err := frRbc.rs.Encode(scalars, frRbc.nbNodes)

	// Broadcast an echo message for each encoding (TODO: should do only one broadcast with all values?)
	for _, Mi := range encodings {
		if Mi == nil {
			return err
		}
		MiBytes, err := Mi.V.MarshalBinary()
		if err != nil {
			return err
		}
		echoInst := createEchoMessage(MiBytes, h, Mi.I)
		err = frRbc.broadcastInstruction(echoInst)
		if err != nil {
			return err
		}
	}
	frRbc.log.Printf("Sent %d echo messages", len(encodings))
	return nil
}

func (frRbc *FourRoundRBC[M]) receiveEcho(msg *typedefs.Message_Echo) error {
	frRbc.RLock()
	defer frRbc.RUnlock()

	// Ignore ECHO message that are not for our index
	if msg.I != frRbc.i {
		return nil
	}

	// Count this message and check if we need to send a READY message
	sendReady := false
	frRbc.echoCount.DoAndSet(string(msg.H), func(count int, ok bool) int {
		// Update the count
		if !ok {
			count = 0
		}
		count += 1

		// If the hash has received enough READY messages, then the threshold only needs be t+1
		// TODO not sure if we don't have sync problems here. Check it
		hashReady := frRbc.checkReadyThreshold(frRbc.readyCounts.GetOrDefault(string(msg.H), 0))

		// Check if we received enough (taking into account if we already received enough ready messages for that hash
		if frRbc.checkEchoThreshold(count+1, hashReady) && !frRbc.sentReady {
			sendReady = true
		}

		return count
	})

	if !sendReady {
		return nil
	}

	// Send the ready message and set sentReady
	inst := createReadyMessage(msg.Mi, msg.H, msg.I)
	err := frRbc.broadcastInstruction(inst)
	if err != nil {
		return err
	}
	frRbc.sentReady = true
	frRbc.log.Printf("Sent Ready message for %x", msg.Mi)
	return nil
}

func (frRbc *FourRoundRBC[M]) receiveReady(msg *typedefs.Message_Ready) bool {
	frRbc.RLock()
	defer frRbc.RUnlock()

	// Update the count of ready message received for that hash and check if a READY message needs to be sent
	sendReady := false
	frRbc.readyCounts.DoAndSet(string(msg.H), func(count int, ok bool) int {
		if !ok {
			return 1
		}

		// If enough READY messages have been received and enough ECHO messages, then send a READY message
		// if not already sent
		echoes, ok := frRbc.echoCount.Get(string(msg.H))
		if ok && !frRbc.sentReady && frRbc.checkReadyThreshold(count+1) && frRbc.checkReadyThreshold(echoes) {
			sendReady = true
		}

		// Return the new count to be set
		frRbc.log.Printf("Received %d ready message(s)", count+1)
		return count + 1
	})

	if sendReady {
		// Send the ready message and set sentReady
		inst := createReadyMessage(msg.Mi, msg.H, msg.I)
		err := frRbc.broadcastInstruction(inst)
		if err != nil {
			// TODO better error handling
			return false
		}
		frRbc.sentReady = true
		frRbc.log.Printf("Sent Ready message for %x", msg.Mi)
	}

	firstTime := false
	finished := false
	var value []M = nil
	frRbc.readyMis.DoAndSet(string(msg.H), func(hashMis map[string]struct{}, ok bool) map[string]struct{} {
		if !ok {
			hashMis = make(map[string]struct{})
		}
		_, notFirst := hashMis[string(msg.Mi)]
		firstTime = !notFirst

		// Put the value in the map
		hashMis[string(msg.Mi)] = struct{}{}
		return hashMis
	})

	// If this value is seen for the first time, add it to T_h and try to reconstruct
	if firstTime {
		MiScalar := frRbc.Scalar()
		err := MiScalar.UnmarshalBinary(msg.Mi)
		if err != nil {
			return false
		}
		frRbc.log.Printf("Received first ready message for %x", msg.Mi)
		frRbc.th = append(frRbc.th, &share.PriShare{
			I: msg.I,
			V: MiScalar,
		})
		frRbc.log.Printf("Got %d messges in th", len(frRbc.th))
		// Try to reconstruct

		value, finished, err = frRbc.reconstruct(msg.H)
		if err != nil {
			frRbc.log.Printf("Failed to reconstruct: %v", err)
		}
	}

	if finished {
		frRbc.finalValue = value
		frRbc.finished = true
		return true
	}

	return false
}

func (frRbc *FourRoundRBC[M]) reconstruct(expHash []byte) ([]M, bool, error) {
	for ri := 0; ri < frRbc.r; ri++ {
		if len(frRbc.th) < 2*frRbc.threshold+frRbc.r+1 {
			// If it is not the case now, it won't be in the next iteration since r increases
			return nil, false, nil
		}
		coefficients, err := frRbc.rs.Decode(frRbc.th, frRbc.threshold+1)
		if err != nil {
			return nil, false, err
		}
		coefficientsMarshalled := make([]M, len(coefficients))
		for i, c := range coefficients {
			b, err := c.MarshalBinary()
			if err != nil {
				return nil, false, err
			}
			m, err := frRbc.marshaller.Unmarshal(b)
			if err != nil {
				return nil, false, err
			}
			coefficientsMarshalled[i] = m
		}
		h, err := frRbc.FreshHash(coefficientsMarshalled)
		if err != nil {
			return nil, false, err
		}

		if bytes.Equal(h, expHash) {
			return coefficientsMarshalled, true, nil

		}
	}
	return nil, false, nil
}

func (frRbc *FourRoundRBC[M]) broadcastInstruction(instruction *typedefs.Instruction) error {
	out, err := proto.Marshal(instruction)
	if err != nil {
		return err
	}

	return frRbc.iface.Broadcast(out)
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

func (frRbc *FourRoundRBC[M]) checkEchoThreshold(count int, hashReady bool) bool {
	if hashReady {
		return count >= (frRbc.threshold + 1)
	}
	return count >= (2*frRbc.threshold + 1)
}

func (frRbc *FourRoundRBC[M]) checkReadyThreshold(count int) bool {
	return count >= (frRbc.threshold + 1)
}
