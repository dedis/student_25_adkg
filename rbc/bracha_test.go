package rbc

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"student_25_adkg/networking"
	"testing"
	"time"
)

type TestNode struct {
	iface networking.NetworkInterface[BrachaMsg]
	rbc   *BrachaRBC[kyber.Scalar]
	stop  bool
}

func NewTestNode(iface networking.NetworkInterface[BrachaMsg], rbc *BrachaRBC[kyber.Scalar]) *TestNode {
	return &TestNode{
		iface: iface,
		rbc:   rbc,
	}
}

type BrachaMsg networking.Message[MessageType, kyber.Scalar]

func NewBrachaMsg(t MessageType, s kyber.Scalar) *BrachaMsg {
	msg := networking.NewMessage[MessageType, kyber.Scalar](t, s)
	return (*BrachaMsg)(msg)
}

func (n *TestNode) HandleMessage(t *testing.T, received BrachaMsg) {
	t.Logf("[%d] Received message type: %d, content: %s", n.iface.GetID(), received.MsgType, received.MsgContent.String())
	msgType, val, send, output, finished := n.rbc.HandleMsg(received.MsgType, received.MsgContent)
	if finished {
		t.Logf("[%d] Algorithm finished, value: %s", n.iface.GetID(), output.String())
	}

	if send {
		t.Logf("[%d] Sendig message of type %d", n.iface.GetID(), msgType)
		msg := *NewBrachaMsg(msgType, val)
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
func TestBrachaSimple(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[BrachaMsg]()
	g := edwards25519.NewBlakeSHA256Ed25519()
	s := g.Scalar().Pick(g.RandomStream())
	threshold := 1
	nbNodes := 3

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		node := NewTestNode(network.JoinNetwork(), NewBrachaRBC(pred, threshold))
		nodes[i] = node
		node.Start(t)
	}

	n1 := nodes[0]
	// Start RBC
	msgType, val := n1.rbc.Deal(s)
	msg := *NewBrachaMsg(msgType, val)
	err := n1.iface.Broadcast(msg)
	require.NoError(t, err)

	time.Sleep(4 * time.Second)

	// Check that all nodes settled on the same correct value and all finished
	for _, n := range nodes {
		val := n.rbc.value
		finished := n.rbc.finished
		require.True(t, finished)
		require.True(t, s.Equal(val))
	}
}
