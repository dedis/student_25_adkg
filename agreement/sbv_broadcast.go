package agreement

import (
	"fmt"
	"student_25_adkg/agreement/typedefs"
	"sync"

	"google.golang.org/protobuf/proto"
)

type SBVBroadcast struct {
	mu            *sync.RWMutex // to lock received
	nParticipants int
	threshold     int
	nodeID        int
	bvManager     *InstanceManager[BVBroadcast, BVBroadcastConfig]
	broadcast     func(proto.Message) error
	auxCh         chan struct{} // can be initialized right to size nParticipants
	received      map[int]int   // 1 value from each node, pid -> binval
	isActive      bool
	queuedMsgs    map[*typedefs.AuxMessage]struct{}
}

type SBVBroadcastConfig struct {
	NParticipants int
	Threshold     int
	NodeID        int
	RoundID       string
	BVManager     *InstanceManager[BVBroadcast, BVBroadcastConfig]
	BroadcastFn   func(proto.Message) error
}

func NewSBVBroadcastFromConfig(conf *SBVBroadcastConfig) *SBVBroadcast {

	return &SBVBroadcast{
		mu:            &sync.RWMutex{},
		nParticipants: conf.NParticipants,
		threshold:     conf.Threshold,
		nodeID:        conf.NodeID,
		bvManager:     conf.BVManager,
		broadcast:     conf.BroadcastFn,
		auxCh:         make(chan struct{}, conf.NParticipants),
		received:      make(map[int]int),
		queuedMsgs:    make(map[*typedefs.AuxMessage]struct{}),
	}
}

// or can return a channel where handleMessage will write
func (s *SBVBroadcast) Propose(sbvID string, binValue int) ([3]bool, [3]bool, error) {
	s.mu.Lock()
	s.isActive = true
	s.mu.Unlock()
	// handle aux messages which could not be processed before but were enque
	s.processQueued()

	bvInst := s.bvManager.GetOrCreate(sbvID)
	notifyCh, err := bvInst.Propose(sbvID, binValue)
	if err != nil {
		return [3]bool{}, [3]bool{}, err
	}

	if bvInst.BinValues.Length() == 0 {
		<-notifyCh
	}

	// take w (random value) from binvalues and aux it
	randomBinVal, _ := bvInst.BinValues.RandomValue()

	msg := typedefs.AuxMessage{SourceNode: int32(s.nodeID), BinValue: int32(randomBinVal), RoundId: sbvID}

	err = s.broadcast(&msg)
	if err != nil {
		return [3]bool{}, [3]bool{}, err
	}

	for range s.auxCh {
		complete, view := AuxViewPredicate(s, s.nParticipants, s.threshold, &bvInst.BinValues)
		if complete {
			return view, bvInst.BinValues.AsBools(), nil
		}
	}

	return [3]bool{}, [3]bool{}, fmt.Errorf(
		"sbv broadcast did not succeed: received aux messages from %d out of %d participants",
		len(s.received), s.nParticipants)
}

func (s *SBVBroadcast) HandleMessage(m *typedefs.AuxMessage) error { // its easier with concrete msg type
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isActive {
		s.queuedMsgs[m] = struct{}{}
		logger.Info().Msgf("sbv for id %s has not started yet at node %d", m.RoundId, s.nodeID)
		return nil
	}

	if _, ok := s.received[int(m.SourceNode)]; ok {
		return fmt.Errorf("redundant AUX from node %d", m.BinValue)
	}
	s.received[int(m.SourceNode)] = int(m.BinValue)

	s.auxCh <- struct{}{}

	return nil
}

// implement BinValsReceiver interface to execute view predicate
func (s *SBVBroadcast) Received() map[int]int {
	return s.received
}

func (s *SBVBroadcast) Lock() {
	s.mu.Lock()
}

func (s *SBVBroadcast) Unlock() {
	s.mu.Unlock()
}

func (s *SBVBroadcast) processQueued() {
	for msg := range s.queuedMsgs {
		go func() {
			err := s.HandleMessage(msg)
			if err != nil {
				logger.Info().Msgf("failed handling AuxMessage: %s from node %d at node %d", msg.String(),
					s.nodeID, msg.SourceNode)
			}
		}()
	}

	// queue will not grow further since the sbv instance is active
	// by the time this function is called
	for msg := range s.queuedMsgs {
		delete(s.queuedMsgs, msg)
	}
}
