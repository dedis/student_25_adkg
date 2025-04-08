package commoncoin

import (
	"fmt"
	"student_25_adkg/networking"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/sign/tbls"
	"go.dedis.ch/kyber/v4/xof/blake2xb"
)

type TestNode struct {
	iface networking.NetworkInterface[CommonCoinMsg]
	coin  *CommonCoin
	stop  bool
}

func NewTestNode(iface networking.NetworkInterface[CommonCoinMsg], coin *CommonCoin) *TestNode {
	return &TestNode{
		iface: iface,
		coin:  coin,
	}
}

type CommonCoinMsg networking.Message[CoinMsgType, CoinMessage]

func NewCommonCoinMsg(t CoinMsgType, s CoinMessage) *CommonCoinMsg {
	msg := networking.NewMessage[CoinMsgType, CoinMessage](t, s)
	return (*CommonCoinMsg)(msg)
}

func (n *TestNode) HandleMessage(t *testing.T, received CommonCoinMsg) {
	t.Logf("[%d] Received message type: %d, content: %s", n.iface.GetID(), received.MsgType, received.MsgContent.String())
	msgType, val, send, output, finished := n.coin.HandleMsg(received.MsgType, received.MsgContent)
	if finished {
		t.Logf("[%d] Algorithm finished, value: %s", n.iface.GetID(), output.String())
	}

	if send {
		t.Logf("[%d] Sendig message of type %d", n.iface.GetID(), msgType)
		msg := *NewCommonCoinMsg(msgType, val)
		err := n.iface.Broadcast(msg)
		require.NoError(t, err)
	}
	t.Logf("[%d] Nothing to do with the handled message", n.iface.GetID())
}

func (n *TestNode) Start(t *testing.T) {
	go func() {
		n.stop = false
		for !n.stop {
			if !n.iface.HasMessage() {
				continue
			}
			received, err := n.iface.Receive()
			if err != nil {
				fmt.Printf("Error receiving message: %s\n", err)
			}
			n.HandleMessage(t, received)
		}
	}()
}

func pred(kyber.Scalar) bool {
	return true
}

// TestBrachaSimple creates a network of 3 nodes with a threshold of 1 and then lets one node start dealing and waits
// sometime for the algorithm to finish and then check all nodes finished and settles on the same value that was dealt
func TestCCSimple(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[CommonCoinMsg]()
	// g := edwards25519.NewBlakeSHA256Ed25519()
	// s := g.Scalar().Pick(g.RandomStream())
	threshold := 1
	nbNodes := 3

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		// localShare share.PriShare, pubCommitment *share.PubPoly for each coin

		seedBytes := []byte(fmt.Sprintf("Hello Common Coin {}"))
		stream := blake2xb.New(seedBytes)
		suite := bn256.NewSuiteRand(stream)
		scheme := tbls.NewThresholdSchemeOnG1(suite)

		secret := suite.G1().Scalar().Pick(stream)
		priPoly := share.NewPriPoly(suite.G2(), threshold, secret, stream)
		pubPoly := priPoly.Commit(suite.G2().Point().Base())

		// sigShares := make([][]byte, 0)
		// for _, x := range priPoly.Shares(nbNodes) {
		// 	sig, err := scheme.Sign(x, msg)
		// 	require.Nil(t, err)
		// 	sigShares = append(sigShares, sig)
		// }

		node := NewTestNode(network.JoinNetwork(), NewCommonCoin(nbNodes, threshold, scheme, *priPoly.Eval(0), pubPoly))
		nodes[i] = node
		node.Start(t)
	}

	// n1 := nodes[0]
	// // Start RBC
	// msgType, val := n1.rbc.Deal(s)
	// msg := *NewCommonCoinMsg(msgType, val)
	// err := n1.iface.Broadcast(msg)
	// require.NoError(t, err)

	// time.Sleep(4 * time.Second)

	// // Check that all nodes settled on the same correct value and all finished
	// for _, n := range nodes {
	// 	val := n.rbc.value
	// 	finished := n.rbc.finished
	// 	require.True(t, finished)
	// 	require.True(t, s.Equal(val))
	// }
}
