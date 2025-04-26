package agreement

import (
	"fmt"
	"strconv"
	"student_25_adkg/agreement/typedefs"
	"sync"

	"google.golang.org/protobuf/proto"
)

type ABA struct {
	mu            *sync.RWMutex // to lock received
	nParticipants int
	threshold     int
	est           int // should be 0 or 1, -1 means no estimate
	round         int
	broadcast     func(proto.Message) error
	nodeID        int
	agreementID   int
	// queuedMsgs    map[types.AuxSetMessage]struct{}
	roundManager *InstanceManager[ABARound, ABARoundConfig]
	isActive     bool
}

type ABAConfig struct {
	NParticipants int
	Threshold     int
	NodeID        int
	BroadcastFn   func(proto.Message) error
	AgreementID   int
	SBVManager    *InstanceManager[SBVBroadcast, SBVBroadcastConfig]
}

func NewABAFromConf(conf *ABAConfig) *ABA {
	roundConfig := &ABARoundConfig{
		NParticipants:     conf.NParticipants,
		Threshold:         conf.Threshold,
		NodeID:            conf.NodeID,
		BroadcastFn:       conf.BroadcastFn,
		AgreementObjectID: conf.AgreementID,
		SBVManager:        *conf.SBVManager,
	}
	roundManager := NewInstanceManager[ABARound, ABARoundConfig](roundConfig, NewABARoundFromConfig,
		func(base *ABARoundConfig, id string) *ABARoundConfig {
			var err error
			base.Round, err = strconv.Atoi(id)
			if err != nil {
				logger.Debug().Msgf("failed to convert %s to int", id)
			}
			return base
		})
	return &ABA{
		mu:            &sync.RWMutex{},
		nParticipants: conf.NParticipants,
		threshold:     conf.Threshold,
		round:         0,
		broadcast:     conf.BroadcastFn,
		nodeID:        conf.NodeID,
		agreementID:   conf.AgreementID,
		// queuedMsgs:    make(map[types.AuxSetMessage]struct{}),
		roundManager: roundManager,
	}
}

type ABARound struct {
	mu            *sync.RWMutex // to lock received
	nParticipants int
	threshold     int
	est           int // should be 0 or 1, -1 means no estimate
	round         int
	views         map[int]*BinSet // round -> stage[0,1,2] -> view
	binValues     *BinSet         // round -> binvalues
	broadcast     func(proto.Message) error
	sbv           *SBVBroadcast
	nodeID        int
	received      map[int]*BinSet // bin -> pid
	auxSetCh      chan struct{}   // round -> chan
	agreementID   int
	sbvManager    InstanceManager[SBVBroadcast, SBVBroadcastConfig]
	decidedCh     chan int
	queuedMsgs    map[*typedefs.AuxSetMessage]struct{}
	isActive      bool
}

type ABARoundConfig struct {
	// RoundID           string
	NParticipants     int
	Threshold         int
	NodeID            int
	Round             int
	BroadcastFn       func(proto.Message) error
	AgreementObjectID int
	Est               int
	SBVManager        InstanceManager[SBVBroadcast, SBVBroadcastConfig]
}

func NewABARoundFromConfig(conf *ABARoundConfig) *ABARound {
	return &ABARound{
		mu:            &sync.RWMutex{},
		nParticipants: conf.NParticipants,
		threshold:     conf.Threshold,
		est:           conf.Est,   // No initial estimate
		round:         conf.Round, // Start at round 0
		views:         make(map[int]*BinSet),
		binValues:     NewBinSet(),
		broadcast:     conf.BroadcastFn,
		sbvManager:    conf.SBVManager,
		nodeID:        conf.NodeID,
		received:      make(map[int]*BinSet),                   // instantiate for each round as well
		auxSetCh:      make(chan struct{}, conf.NParticipants), // instantiate with nParticipant for each round
		agreementID:   conf.AgreementObjectID,
		decidedCh:     make(chan int, 1),
		queuedMsgs:    make(map[*typedefs.AuxSetMessage]struct{}),
	}
}

func (a *ABARound) pidsByBinVals() map[int][]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	res := make(map[int][]int)

	for pid, binset := range a.received {
		binvals := binset.AsInts()
		for _, val := range binvals {
			res[val] = append(res[val], pid)
		}
	}
	return res
}

// roundID format: string: "ABA" + string(on which node acss do we agree) + "r" + string(roundID)

// return est and nil
func (a *ABARound) Start(est int) (int, error) {
	a.mu.Lock()
	a.isActive = true
	a.mu.Unlock()

	a.processQueued()

	a.est = est
	// SBV_Broadcast(est) SBV_Broadcast Stage[ri, 0](est)
	curStageRoundID := ABARoundUID{AgreementID: a.agreementID, Round: a.round, Stage: 0}
	sbvInst := a.sbvManager.GetOrCreate(curStageRoundID.String())
	view0, binValues, err := sbvInst.Propose(curStageRoundID.String(), a.est)
	if err != nil {
		return a.est, fmt.Errorf("sbv broadcast failed %w", err)
	}

	a.views[0], err = NewBinSet().FromBools(view0)
	if err != nil {
		return a.est, fmt.Errorf("error during aba round %d, err: %s", a.round, err.Error())
	}
	a.binValues, err = NewBinSet().FromBools(binValues)
	if err != nil {
		return a.est, fmt.Errorf("error during aba round %d, err: %s", a.round, err.Error())
	}

	// broadcast auxset
	msg := typedefs.AuxSetMessage{
		RoundId:    curStageRoundID.String(),
		SourceNode: int32(a.nodeID),
		View:       ConvertToView(a.views[0].AsBools()),
	}
	err = a.broadcast(&msg)
	if err != nil {
		return a.est, fmt.Errorf("auxset broadcast failed %w", err)
	}

	for {
		<-a.auxSetCh
		complete, view := AuxSetViewPredicate(a.pidsByBinVals(), a.nParticipants, a.threshold, a.binValues)
		if complete {
			a.views[1], err = NewBinSet().FromBools(view)
			if err != nil {
				return a.est, fmt.Errorf("error during aba round %d, err: %s", a.round, err.Error())
			}
			break
		}
	}

	if a.views[1].Length() == 1 {
		a.est = a.views[1].AsInts()[0]
	} else {
		a.est = UndecidedBinVal
	}

	// second SBV broadcast

	curStageRoundID = ABARoundUID{AgreementID: a.agreementID, Round: a.round, Stage: 2}
	sbvInst = a.sbvManager.GetOrCreate(curStageRoundID.String())
	view2, _, err := sbvInst.Propose(curStageRoundID.String(), a.est)

	if err != nil {
		return a.est, fmt.Errorf("sbv broadcast failed %w", err)
	}

	a.views[2], err = NewBinSet().FromBools(view2)
	if err != nil {
		return a.est, fmt.Errorf("error during aba round %d, err: %s", a.round, err.Error())
	}

	var coinVal int // TODO = commoncoin.random()
	if a.views[2].Length() == 1 &&
		!a.views[2].ContainsUndecided() {
		a.est = a.views[2].AsInts()[0]
		// TODO decide(v) if not yet done, for now just log
		// So decide != return and this thing keeps spinning? Like forever? Yes, a channel
		a.decidedCh <- a.est
		logger.Debug().Msgf("DECIDED %d at round %d node %d", a.est, a.round, a.nodeID)
	} else if a.views[2].Length() == 2 &&
		a.views[2].ContainsUndecided() {
		if a.views[2].ContainsZero() {
			a.est = Zero
		} else if a.views[2].ContainsOne() {
			a.est = One
		}
		// a.est = a.views[a.round][2].AsInts()[0] // 0 or 1
	} else if a.views[2].ContainsUndecided() {
		logger.Debug().Msgf("DECIDED WITH COIN: Est %d, coinval %d at round %d node %d", a.est, coinVal, a.round, a.nodeID)
		a.est = coinVal
	}

	logger.Info().Msgf("humus node %d decided", a.nodeID)
	close(a.decidedCh)
	a.mu.Lock()
	a.isActive = false
	a.mu.Unlock()
	return a.est, nil
}

func (a *ABARound) HandleMessage(msg *typedefs.AuxSetMessage) error {
	// should not happen,
	// instead should go into the queue
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.isActive {
		a.queuedMsgs[msg] = struct{}{}
		return nil
	}

	if _, ok := a.received[int(msg.SourceNode)]; ok {
		return fmt.Errorf("redundant AUX from node %d", msg.SourceNode)
	} else {
		var err error
		viewBinSet, err := ConvertFromView(msg.View)
		if err != nil {
			return err
		}
		a.received[int(msg.SourceNode)], _ = NewBinSet().FromBools(viewBinSet)
	}

	a.auxSetCh <- struct{}{}
	return nil
}

func (a *ABARound) processQueued() {
	for msg := range a.queuedMsgs {
		go func() {
			err := a.HandleMessage(msg)
			if err != nil {
				logger.Info().Msgf("failed handling AuxSetMessage: %s from node %d at node %d", msg.String(),
					a.nodeID, msg.SourceNode)
			}
		}()
	}

	// queue will not grow further since the aba instance is active
	// by the time this function is called
	for msg := range a.queuedMsgs {
		delete(a.queuedMsgs, msg)
	}
}

func (a *ABA) Propose(binValue int) (int, error) {
	a.mu.Lock()
	a.isActive = true
	a.mu.Unlock()
	est := binValue
	var binVal int
	for {
		a.mu.Lock()
		a.round++
		abaRoundInst := a.roundManager.GetOrCreate(strconv.Itoa(a.round))
		a.mu.Unlock()
		est, _ = abaRoundInst.Start(est)
		v, ok := <-abaRoundInst.decidedCh
		if !ok {
			continue
		}
		binVal = v
		break
		//  spinning further does not make sense, right?
	}

	return binVal, nil
}

func (a *ABA) SafeGetRound() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.round
}

func (a *ABA) HandleMessage(m *typedefs.AuxSetMessage) error {
	// save to queue if not active or round less then msg round. Queue will be read at each abaRoundInst.Start(est)
	abaInstStageRoundID, err := ABARoundUIDFromString(m.RoundId)
	if err != nil {
		return err
	}

	a.mu.Lock()
	if !a.isActive {
		logger.Debug().Msgf("aba number %d has not yet been started at node %d",
			abaInstStageRoundID.AgreementID, a.nodeID)
		return nil
	}

	if a.round < abaInstStageRoundID.Round {
		logger.Debug().Msgf("aba number %d has not yet been started at node %d",
			abaInstStageRoundID.AgreementID, a.nodeID)
		return nil
	}
	a.mu.Unlock()

	abaRoundInst := a.roundManager.GetOrCreate(strconv.Itoa(abaInstStageRoundID.Round))
	err = abaRoundInst.HandleMessage(m)
	if err != nil {
		logger.Error().Msgf("failed handling AuxMessage: %s from peer %d", m.String(), m.SourceNode)
		return err
	}

	return nil
}
