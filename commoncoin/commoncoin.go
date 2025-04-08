package commoncoin

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

	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/sign"
)

// async def shared_coin(sid, pid, n, f, pk, sk, broadcast, receive):
// """A shared coin based on threshold signatures

// :param sid: a unique instance id
// :param pid: my id number
// :param N: number of parties
// :param f: fault tolerance, :math:`f+1` shares needed to get the coin
// :param PK: ``boldyreva.TBLSPublicKey``
// :param SK: ``boldyreva.TBLSPrivateKey``
// :param broadcast: broadcast channel
// :param receive: receive channel
// :return: a function ``getCoin()``, where ``getCoin(r)`` blocks"""

// can I use kyber.sign.tbls as boldyreva?
// NewThresholdSchemeOnG1 -> Sign (as boldyreva.pk.hash) and  VerifyPartial
// as boldyreva.pk.verify

const COIN_MSG_STR = "COIN"

type TossRoundType int64

func (t TossRoundType) toString() string {
	return strconv.FormatInt(int64(t), 10)
}

type CoinMessage struct {
	sourceNode int
	round      TossRoundType // does it have to be 64?
	sigShare   []byte
	// pubShare   *share.PubPoly no need to pass, I already know it
}

func (c *CoinMessage) String() string {
	return fmt.Sprintf("Coin round %d from node %d", c.round, c.sourceNode)
}

type CommonCoin struct {
	nParticipants int // number of parties
	// f int // fault tolerance
	scheme sign.ThresholdScheme

	// func (s *scheme) Sign(private *share.PriShare, msg []byte) ([]byte, error) {
	localShare share.PriShare

	// func (s *scheme) VerifyPartial(public *share.PubPoly, msg, sig []byte) error {
	pubCommitment *share.PubPoly

	// should I actually store shares and check
	// if a redundant one received and it not equal the old one?
	// round -> node ID -> sigShare
	received map[TossRoundType]map[int][]byte

	// also receive public shares?

	threshold int
}

func NewCommonCoin(
	nParticipants int,
	threshold int,
	scheme sign.ThresholdScheme,
	localShare share.PriShare,
	pubCommitment *share.PubPoly,
) *CommonCoin {
	return &CommonCoin{
		nParticipants: nParticipants,
		threshold:     threshold,
		scheme:        scheme,
		localShare:    localShare,
		pubCommitment: pubCommitment,
		received:      make(map[TossRoundType]map[int][]byte),
	}
}

// Should be done in another way? (collisions)
type CoinMsgType int

const (
	FLIPPED CoinMsgType = iota
	ECHO
	READY
)

// HandleMsg implements the method from the RBC interface
func (coin *CommonCoin) HandleMsg(msgType CoinMsgType, val CoinMessage) (CoinMsgType, CoinMessage, bool, CoinMessage, bool) {
	// switch msgType {
	// case PROPOSE:
	// 	echo := rbc.receivePropose(val)
	// 	return ECHO, val, echo, val, false
	// case ECHO:
	// 	ready := rbc.receiveEcho(val)
	// 	return READY, val, ready, val, false
	// case READY:
	// 	finished, ready := rbc.receiveReady(val)
	// 	if finished {
	// 		return msgType, val, false, val, true
	// 	}
	// 	return msgType, val, ready, val, false
	// default:
	// 	return msgType, val, false, val, false
	// }
	return msgType, val, false, val, false
}

// to initialize with correct parameters in the agreement there should be something like
// func createCoin() where the correct secret share is calculated:
// So for node i for ABA of j (set T_j) it will be the sum of keys in the set.

// Where does T_j come from? Idk, ACSS.

func (coin *CommonCoin) collectRoundSigs(round TossRoundType) [][]byte {
	// len(coin.received[round]) == coin.nParticipants?
	// len(sigs) == coin.threshold + 1?
	var sigs [][]byte
	for i := 0; i < len(coin.received[round]); i++ {
		if sig, ok := coin.received[round][i]; ok {
			sigs = append(sigs, sig)
		}
	}

	return sigs
}

// setup logger?
// pass params instead of msg and rename to ~recv to separate from the network more?
// apart from error return a respond if any
// (bool, int, error)
func (coin *CommonCoin) HandleMessage(msg *CoinMessage) error {
	// which index will IndexOf on signature give me? (which share does it use -
	// since it's a fucking sum of shares I guess it should be equal to the index source node
	// so it may be worth it to add a check)
	sourceNodeIndex, err := coin.scheme.IndexOf(msg.sigShare)
	if err != nil || msg.sourceNode != sourceNodeIndex {
		return fmt.Errorf("node ids from msg and sig don't match: %d != %d",
			msg.sourceNode, sourceNodeIndex)
	}

	if msg.sourceNode > coin.nParticipants {
		return fmt.Errorf("invalid node id: %d > %d number of participants",
			msg.sourceNode, coin.nParticipants)
	}
	if msg.round < 0 {
		return fmt.Errorf("common coin round can't be negative %d", msg.round)
	}

	// btw they hash the msg before signing. Why?
	coinRoundMsg := []byte(COIN_MSG_STR + msg.round.toString())
	err = coin.scheme.VerifyPartial(coin.pubCommitment, coinRoundMsg, msg.sigShare)
	if err != nil {
		return fmt.Errorf("partial signature verification failed for node %d", msg.sourceNode)
	}

	// check if msg is redundant (can it happen? not in out setting)
	if roundMap, ok := coin.received[msg.round]; ok {
		if _, ok := roundMap[msg.sourceNode]; ok {
			return fmt.Errorf("redundant coin sig received: %s from %d for round %d",
				string(msg.sigShare), msg.sourceNode, msg.round)
		}
	}
	// underlying pointer assignment but msg.sigShare should not change
	coin.received[msg.round][msg.sourceNode] = msg.sigShare

	if len(coin.received[msg.round]) == coin.threshold+1 {
		// Recover(public *share.PubPoly, msg []byte, sigs [][]byte, t, n int) ([]byte, error)
		thresholdSig, err := coin.scheme.Recover(coin.pubCommitment, coinRoundMsg, coin.collectRoundSigs(msg.round), coin.threshold, coin.nParticipants)
		if err != nil {
			return fmt.Errorf("failed to recover threshold sig for coin round %d", msg.round)
		}

		// is coin.pubKey.Commit()? Takes a point
		err = coin.scheme.VerifyRecovered(coin.pubCommitment.Commit(), coinRoundMsg, thresholdSig)
		if err != nil {
			return fmt.Errorf("threshold signature verification failed for coin round %d", msg.round)
		}

		// return the bit and termination bool?
	}

	return nil
	// return false, 0, nil
}

func (coin *CommonCoin) signPartial(round TossRoundType) ([]byte, error) {
	// return a msg to broadcast
	// round?
	// generate new keys? - I guess not

	// async def get_coin(round):
	// """Gets a coin.

	// :param round: the epoch/round.
	// :returns: a coin.

	// """
	// # I have to do mapping to 1..l
	// h = pk.hash_message(str((sid, round)))
	// logger.debug(
	// 	f"[{pid}] broadcast {('COIN', round, sk.sign(h))}",
	// 	extra={"nodeid": pid, "epoch": round},
	// )
	// broadcast(("COIN", round, serialize(sk.sign(h))))
	// return await output_queue[round].get()

	coinRoundMsg := []byte(COIN_MSG_STR + round.toString())

	// Sign(private *share.PriShare, msg []byte) ([]byte, error)
	signedMsg, err := coin.scheme.Sign(&coin.localShare, coinRoundMsg)
	if err != nil {
		return []byte{}, fmt.Errorf("partial signing failed for coin round %d", round)
	}

	return signedMsg, nil
}

func (coin *CommonCoin) flip(round TossRoundType) (int, error) {
	// TODO run everything
	return 0, nil // TODO return real values
}
