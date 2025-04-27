package agreement

// coin threshold is n-t

// if any shared randomness is required for
// the jth ABA instance, nodes use Tj to generate the shared
// randomness. More specifically, let uj be the following value
// u_i_j = ∑s_k
// 	   k∈Tj

// What that means for me: just assume the coin at node i
// get's secret shares u_i_j of common secret u_j
// and each node can reconstuct g^u_i_j and g^u_j
// (how? By multiplying commitments? Soo I can assume I have g^u_j already
// and don't need to pass it in the coin message?)

// func (s *scheme) VerifyPartial(public *share.PubPoly, msg, sig []byte) error {
// 	sh := SigShare(sig)
// 	i, err := sh.Index()
// 	if err != nil {
// 		return err
// 	}
// 	return s.Scheme.Verify(public.Eval(uint32(i)).V, msg, sh.Value())
// }

// That's why verify needs the index. And it's inside the sig, what?
// There is and index encoded INSIDE OF EACH SHARE.
// 	if err := binary.Write(buf, binary.BigEndian, uint16(private.I)); err != nil {

// async def run_binary_agreement(config, pbk, pvk, n, f, nodeid):
//     from adkg.broadcast.commoncoin import shared_coin
// import random
//
// sid_c = "sid_coin"
// sid_ba = "sid_ba"
// h = pk.hash_message(str((sid, r)))
// pk.verify_share(sig, i, h)
// no idea what is this sid for but looks quite useless

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
	broadcastID   string
	mu            sync.Mutex
	nParticipants int32 // number of parties
	scheme        sign.ThresholdScheme
	localShare    *share.PriShare
	pubCommitment *share.PubPoly
	// round -> node ID -> sigShare
	// received map[uint64]map[int32][]byte
	// node ID -> sigShare
	received map[int32][]byte
	notifyCh chan struct{}
	// also receive public shares?
	threshold  int32
	broadcast  func(proto.Message) error
	result     int
	nodeID     int32
	isActive   bool
	queuedMsgs map[*typedefs.CoinMessage]struct{}
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
		nParticipants: nParticipants,
		threshold:     threshold,
		scheme:        scheme,
		localShare:    localShare,
		pubCommitment: pubCommitment,
		// received:      make(map[uint64]map[int32][]byte),
		received:    make(map[int32][]byte),
		notifyCh:    make(chan struct{}, 1),
		broadcast:   broadcast,
		nodeID:      nodeID,
		queuedMsgs:  make(map[*typedefs.CoinMessage]struct{}),
		broadcastID: broadcastID,
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
		nParticipants: int32(conf.NParticipants),
		threshold:     int32(conf.Threshold),
		scheme:        conf.Scheme,
		localShare:    conf.LocalShare,
		pubCommitment: conf.PubCommitment,
		// received:      make(map[uint64]map[int32][]byte),
		received:    make(map[int32][]byte),
		notifyCh:    make(chan struct{}, 1),
		broadcast:   conf.BroadcastFn,
		nodeID:      int32(conf.NodeID),
		queuedMsgs:  make(map[*typedefs.CoinMessage]struct{}),
		broadcastID: conf.BroadcastID,
	}
}

// to initialize with correct parameters in the agreement there should be something like
// func createCoin() where the correct secret share is calculated:
// So for node i for ABA of j (set T_j) it will be the sum of keys in the set.

// Where does T_j come from? Idk, ACSS.

func (coin *CommonCoin) collectSigs() [][]byte {
	// sigs := make([][]byte, 0)
	var sigs [][]byte
	// for i := 0; i < int(coin.nParticipants); i++ {
	// 	if sig, ok := coin.received[int32(i)]; ok {
	// 		logger.Debug().Msgf("in append %d node %d", i, coin.nodeID)
	// 		sigs = append(sigs, sig)
	// 	}
	// }

	for _, sig := range coin.received {
		sigs = append(sigs, sig)
	}

	// logger.Debug().Msgf("COLLECT RET len(rec) %d len(sig) %d node %d", len(coin.received), len(sigs), coin.nodeID)

	return sigs
}

func (coin *CommonCoin) HandleMessage(msg *typedefs.CoinMessage) error {
	// which index will IndexOf on signature give me? (which share does it use -
	// since it's a fucking sum of shares I guess it should be equal to the index source node
	// so it may be worth it to add a check)
	sourceNodeIndex, err := coin.scheme.IndexOf(msg.SigShare)
	if err != nil || msg.SourceNode != int32(sourceNodeIndex) {
		return fmt.Errorf("node ids from msg and sig don't match: %d != %d",
			msg.SourceNode, sourceNodeIndex)
	}

	if msg.SourceNode > coin.nParticipants {
		return fmt.Errorf("invalid node id: %d > %d number of participants",
			msg.SourceNode, coin.nParticipants)
	}

	// btw they hash the msg before signing. Why?
	// coinRoundMsg := []byte(COIN_MSG_STR + RoundtoString(msg.Round))
	coinRoundMsg := []byte(msg.BroadcastId)
	err = coin.scheme.VerifyPartial(coin.pubCommitment, coinRoundMsg, msg.SigShare)
	if err != nil {
		return fmt.Errorf("partial signature verification failed for node %d", msg.SourceNode)
	}

	coin.mu.Lock()
	defer coin.mu.Unlock()
	// check if msg is redundant (can it happen? not in our setting)
	if _, ok := coin.received[msg.SourceNode]; ok {
		return fmt.Errorf("redundant coin sig received: %s from %d for round %d",
			string(msg.SigShare), msg.SourceNode, msg.Round)
	}

	coin.received[msg.SourceNode] = msg.SigShare

	if len(coin.received) == int(coin.threshold) {
		thresholdSig, err := coin.scheme.Recover(coin.pubCommitment, coinRoundMsg, coin.collectSigs(), int(coin.threshold), int(coin.nParticipants))

		if err != nil {
			logger.Error().Msgf("failed to recover threshold sig for coin id %s", msg.BroadcastId)
			return fmt.Errorf("failed to recover threshold sig for coin id %s", msg.BroadcastId)
		}

		err = coin.scheme.VerifyRecovered(coin.pubCommitment.Commit(), coinRoundMsg, thresholdSig)
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

	// Sign(private *share.PriShare, msg []byte) ([]byte, error)
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
		// Round:       uint64(round),
		SigShare: sigShare,
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
