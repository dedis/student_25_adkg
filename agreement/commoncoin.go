package agreement

// coin threshold is n-t

// From the paper:
// if any shared randomness is required for
// the jth ABA instance, nodes use Tj to generate the shared
// randomness. More specifically, let uj be the following value
// u_j = ∑s_k
// 	   k∈Tj

// u_i_j = ∑s_k_i
// 	   k∈Tj

// ...our ACSS to allow each node to locally compute g^u_j and
// g^u_i_j for all i and j to finish the coin tossing setup.

// My implementation:
// assume node x (after j-th ACSS finished)
// knows public g^u_j and g^u_i_j for all i's.
// They are passed to coin in pubCommitments *share.PubPoly
// u_i_j is the localShare *share.PriShare

import (
	"fmt"
	"strconv"
	"student_25_adkg/agreement/typedefs"
	"sync"

	"github.com/rs/zerolog/log"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/sign"
	"google.golang.org/protobuf/proto"
)

func RoundtoString(r uint64) string {
	return strconv.FormatInt(int64(r), 10)
}

type CommonCoin struct {
	broadcastID    string
	mu             sync.Mutex
	nParticipants  int32
	scheme         sign.ThresholdScheme
	localShare     *share.PriShare
	pubCommitments *share.PubPoly
	received       map[int32][]byte
	notifyCh       chan struct{}
	threshold      int32
	broadcast      func(proto.Message) error
	result         int
	nodeID         int32
	isActive       bool
	queuedMsgs     map[*typedefs.CoinMessage]struct{}
}

func NewCommonCoin(
	nParticipants int32,
	threshold int32,
	scheme sign.ThresholdScheme,
	localShare *share.PriShare,
	pubCommitment *share.PubPoly,
	broadcast func(proto.Message) error,
	nodeID int32,
	broadcastID string,
) *CommonCoin {
	return &CommonCoin{
		nParticipants:  nParticipants,
		threshold:      threshold,
		scheme:         scheme,
		localShare:     localShare,
		pubCommitments: pubCommitment,
		received:       make(map[int32][]byte),
		notifyCh:       make(chan struct{}, 1),
		broadcast:      broadcast,
		nodeID:         nodeID,
		queuedMsgs:     make(map[*typedefs.CoinMessage]struct{}),
		broadcastID:    broadcastID,
	}
}

type CommonCoinConfig struct {
	NParticipants int
	Threshold     int
	NodeID        int
	BroadcastFn   func(proto.Message) error
	LocalShare    *share.PriShare
	PubCommitment *share.PubPoly
	Scheme        sign.ThresholdScheme
	BroadcastID   string
}

func NewCommonCoinFromConfig(conf *CommonCoinConfig) *CommonCoin {
	return &CommonCoin{
		nParticipants:  int32(conf.NParticipants),
		threshold:      int32(conf.Threshold),
		scheme:         conf.Scheme,
		localShare:     conf.LocalShare,
		pubCommitments: conf.PubCommitment,
		received:       make(map[int32][]byte),
		notifyCh:       make(chan struct{}, 1),
		broadcast:      conf.BroadcastFn,
		nodeID:         int32(conf.NodeID),
		queuedMsgs:     make(map[*typedefs.CoinMessage]struct{}),
		broadcastID:    conf.BroadcastID,
	}
}

func (coin *CommonCoin) collectSigs() [][]byte {
	var sigs [][]byte
	for _, sig := range coin.received {
		sigs = append(sigs, sig)
	}
	return sigs
}

func (coin *CommonCoin) HandleMessage(msg *typedefs.CoinMessage) error {
	sourceNodeIndex, err := coin.scheme.IndexOf(msg.SigShare)
	if err != nil || msg.SourceNode != int32(sourceNodeIndex) {
		return fmt.Errorf("node ids from msg and sig don't match: %d != %d",
			msg.SourceNode, sourceNodeIndex)
	}

	if msg.SourceNode > coin.nParticipants {
		return fmt.Errorf("invalid node id: %d > %d number of participants",
			msg.SourceNode, coin.nParticipants)
	}

	coinRoundMsg := []byte(msg.BroadcastId)
	err = coin.scheme.VerifyPartial(coin.pubCommitments, coinRoundMsg, msg.SigShare)
	if err != nil {
		return fmt.Errorf("partial signature verification failed for node %d", msg.SourceNode)
	}

	coin.mu.Lock()
	defer coin.mu.Unlock()
	if _, ok := coin.received[msg.SourceNode]; ok {
		return fmt.Errorf("redundant coin sig received: %s from %d for round %d",
			string(msg.SigShare), msg.SourceNode, msg.Round)
	}

	coin.received[msg.SourceNode] = msg.SigShare

	if len(coin.received) == int(coin.threshold) {
		thresholdSig, err := coin.scheme.Recover(coin.pubCommitments, coinRoundMsg, coin.collectSigs(), int(coin.threshold), int(coin.nParticipants))

		if err != nil {
			logger.Error().Msgf("failed to recover threshold sig for coin id %s", msg.BroadcastId)
			return fmt.Errorf("failed to recover threshold sig for coin id %s", msg.BroadcastId)
		}

		err = coin.scheme.VerifyRecovered(coin.pubCommitments.Commit(), coinRoundMsg, thresholdSig)
		if err != nil {
			return fmt.Errorf("threshold signature verification failed for coin round %d", msg.Round)
		}

		coin.result = int((thresholdSig[0] >> 7) & 1) // take first bit
		coin.notifyCh <- struct{}{}
	}

	return nil
}

func (coin *CommonCoin) signPartial() ([]byte, error) {
	coinRoundMsg := []byte(coin.broadcastID)
	signedMsg, err := coin.scheme.Sign(coin.localShare, coinRoundMsg)
	if err != nil {
		return []byte{}, fmt.Errorf("partial signing failed for coin id %s", coin.broadcastID)
	}

	return signedMsg, nil
}

func (coin *CommonCoin) Flip() (int, error) {
	coin.mu.Lock()
	coin.isActive = true
	coin.mu.Unlock()
	sigShare, err := coin.signPartial()
	if err != nil {
		log.Error().Msgf("failed to partially sign at node %d", coin.nodeID)
		return 0, err
	}
	coinMsg := &typedefs.CoinMessage{
		BroadcastId: coin.broadcastID,
		SourceNode:  coin.nodeID,
		SigShare:    sigShare,
	}
	err = coin.broadcast(coinMsg)
	if err != nil {
		log.Error().Msgf("failed to broadcast coin msg from node %d", coin.nodeID)
		return 0, err
	}
	coin.processQueued()

	<-coin.notifyCh

	return coin.result, nil
}

func (s *CommonCoin) processQueued() {
	for msg := range s.queuedMsgs {
		go func() {
			err := s.HandleMessage(msg)
			if err != nil {
				logger.Info().Msgf("failed handling CoinMessage: %s from node %d at node %d", msg.String(),
					s.nodeID, msg.SourceNode)
			}
		}()
	}

	// queue will not grow further since the coin instance is active
	// by the time this function is called
	for msg := range s.queuedMsgs {
		delete(s.queuedMsgs, msg)
	}
}
