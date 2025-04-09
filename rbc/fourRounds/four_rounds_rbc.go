package fourRounds

import (
	"bytes"
	"context"
	"fmt"
	"go.dedis.ch/kyber/v4"
	"golang.org/x/xerrors"
	"google.golang.org/protobuf/proto"
	"hash"
	"log"
	"os"
	"student_25_adkg/rbc"
	"student_25_adkg/rbc/fourRounds/typedefs"
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
	threshold int
	sentReady bool
	sync.RWMutex
	miCounts    tools.ConcurrentMap[string, map[string]int]
	readyCounts tools.ConcurrentMap[string, int]
	readyMis    tools.ConcurrentMap[string, map[string]struct{}]
	th          []*tools.Tuple[int32, []byte]
	kyber.Group
	n          int
	r          int
	finalValue []M
	finished   bool
	log        *log.Logger
	i          int32
}

func NewFourRoundRBC[M any](pred func([]M) bool, h hash.Hash, threshold int,
	iface rbc.AuthenticatedMessageStream,
	marshaller Marshaller[M], group kyber.Group, nbNodes, r int, i int32) *FourRoundRBC[M] {
	return &FourRoundRBC[M]{
		iface:       iface,
		marshaller:  marshaller,
		pred:        pred,
		Hash:        h,
		threshold:   threshold,
		sentReady:   false,
		RWMutex:     sync.RWMutex{},
		miCounts:    *tools.NewConcurrentMap[string, map[string]int](),
		readyCounts: *tools.NewConcurrentMap[string, int](),
		readyMis:    *tools.NewConcurrentMap[string, map[string]struct{}](),
		th:          make([]*tools.Tuple[int32, []byte], 0),
		Group:       group,
		n:           nbNodes,
		r:           r,
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
		frRbc.log.Printf("Received echo message with i %d and msg: %s", op.EchoInst.I, string(op.EchoInst.Mi))
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
	encodings := RSEnc(scalars, frRbc.n, frRbc.Group)

	// Broadcast an echo message for each encoding (TODO: should do only one broadcast with all values?)
	for i, Mi := range encodings {
		if Mi == nil {
			return err
		}
		MiBytes, err := Mi.MarshalBinary()
		if err != nil {
			return err
		}
		echoInst := createEchoMessage(MiBytes, h, int32(i))
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

	if msg.I != frRbc.i {
		return nil
	}

	sendReady := false
	// Check the count for this Mi for this hash
	frRbc.miCounts.DoAndSet(string(msg.H), func(m map[string]int, ok bool) map[string]int {
		// This is a new message hash we have never seen yet
		if !ok {
			m = make(map[string]int)
		}

		// Update the count
		count, ok := m[string(msg.Mi)]
		if !ok {
			// This is a share of the message we have never seen
			count = 0
		}
		m[string(msg.Mi)] = count + 1

		// If the hash has received enough READY messages, then the threshold only needs be t+1
		// TODO not sure if we don't have sync problems here. Check it
		hashReady := frRbc.checkReadyThreshold(frRbc.readyCounts.GetOrDefault(string(msg.H), 0))

		// Check if we received enough (taking into account if we already received enough ready messages for that hash
		if frRbc.checkEchoThreshold(count+1, hashReady) && !frRbc.sentReady {
			sendReady = true
		}
		return m
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
	frRbc.log.Printf("Sent Ready message for %s", string(msg.Mi))
	return nil
}

func (frRbc *FourRoundRBC[M]) receiveReady(msg *typedefs.Message_Ready) bool {
	frRbc.RLock()
	defer frRbc.RUnlock()

	// Update the count of ready message received for that hash
	frRbc.readyCounts.DoAndSet(string(msg.H), func(count int, ok bool) int {
		if !ok {
			return 1
		}
		// Return the new count to be set
		frRbc.log.Printf("Received %d ready message(s)", count+1)
		return count + 1
	})

	firstTime := false
	finished := false
	var value []M = nil
	frRbc.readyMis.DoAndSet(string(msg.H), func(hashMis map[string]struct{}, ok bool) map[string]struct{} {
		if !ok {
			hashMis = make(map[string]struct{})
		}
		_, notFirst := hashMis[string(msg.Mi)]
		firstTime = !notFirst
		frRbc.log.Printf("Received ready message for %s", string(msg.Mi))

		// Put the value in the map
		hashMis[string(msg.Mi)] = struct{}{}
		return hashMis
	})

	// If this value is seen for the first time, add it to T_h and try to reconstruct
	if firstTime {
		frRbc.th = append(frRbc.th, tools.NewTuple(msg.I, msg.Mi))
		frRbc.log.Printf("Got %d messges in th", len(frRbc.th))
		// Try to reconstruct
		var err error
		value, finished, err = frRbc.reconstruct(msg.H)
		if err != nil {
			frRbc.log.Printf("Failed to reconstruct: %v", err)
		} else {
			frRbc.log.Printf("Reconstruction finished: %t", finished)
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
	scalars := make([]kyber.Scalar, len(frRbc.th))
	for i, tuple := range frRbc.th {
		scalar := frRbc.Group.Scalar().One()
		err := scalar.UnmarshalBinary(tuple.GetEl2())
		if err != nil {
			return nil, false, err
		}
		scalars[i] = scalar
	}

	for ri := 0; ri < frRbc.r; ri++ {
		if len(frRbc.th) < 2*frRbc.threshold+frRbc.r+1 {
			// If it is not the case now, it won't be in the next iteration since r increases
			return nil, false, nil
		}
		coeffs, err := RSDec(scalars, 2 < l, frRbc.r, frRbc.Group)
		if err != nil {
			return nil, false, err
		}
		coeffsM := make([]M, len(coeffs))
		for i, c := range coeffs {
			b, err := c.MarshalBinary()
			if err != nil {
				return nil, false, err
			}
			m, err := frRbc.marshaller.Unmarshal(b)
			if err != nil {
				return nil, false, err
			}
			coeffsM[i] = m
		}
		h, err := frRbc.FreshHash(coeffsM)
		if err != nil {
			return nil, false, err
		}

		if bytes.Equal(h, expHash) {
			return coeffsM, true, nil

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

func createReadyMessage(mi, h []byte, i int32) *typedefs.Instruction {
	readyMsg := &typedefs.Message_Ready{
		Mi: mi,
		H:  h,
		I:  i,
	}
	msg := &typedefs.Message{Op: &typedefs.Message_ReadyInst{ReadyInst: readyMsg}}
	inst := &typedefs.Instruction{Operation: msg}
	return inst
}

func createEchoMessage(mi, h []byte, i int32) *typedefs.Instruction {
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
