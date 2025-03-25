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
	g     kyber.Group
	iface networking.NetworkInterface[[]byte]
	rbc   *BrachaRBC[kyber.Scalar]
	stop  bool
}

func NewTestNode(iface networking.NetworkInterface[[]byte], rbc *BrachaRBC[kyber.Scalar]) *TestNode {
	return &TestNode{
		iface: iface,
		rbc:   rbc,
	}
}

type BrachaMsg networking.Message[MessageType, []byte]

func NewBrachaMsg(t MessageType, s []byte) *BrachaMsg {
	msg := networking.NewMessage[MessageType, []byte](t, s)
	return (*BrachaMsg)(msg)
}

func (n *TestNode) HandleMessage(t *testing.T, received RBCMessage[kyber.Scalar]) {
	t.Logf("[%d] Received message type: %d, content: %s", n.iface.GetID(), received.Type(), received.Content())
	msg, finished := n.rbc.HandleMsg(received)
	if finished {
		output := n.rbc.value
		t.Logf("[%d] Algorithm finished, value: %s", n.iface.GetID(), output.String())
	}

	if msg != nil {
		t.Logf("[%d] Sendig message of type %d", n.iface.GetID(), msg.Type())
		msgBytes, err := n.rbc.Marshal(msg)
		require.NoError(t, err)
		err = n.iface.Broadcast(msgBytes)
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
			rbcMessage, err := n.rbc.Unmarshal(received)
			require.NoError(t, err)
			n.HandleMessage(t, *rbcMessage)
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
	network := networking.NewFakeNetwork[[]byte]()
	g := edwards25519.NewBlakeSHA256Ed25519()
	s := g.Scalar().Pick(g.RandomStream())
	threshold := 1
	nbNodes := 3

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		node := NewTestNode(network.JoinNetwork(), NewBrachaRBC(pred, threshold, func(scalar kyber.Scalar) ([]byte, error) {
			data, err := scalar.MarshalBinary()
			return data, err
		}, func(bytes []byte) (kyber.Scalar, error) {
			s := g.Scalar()
			err := s.UnmarshalBinary(bytes)
			return s, err
		}))
		nodes[i] = node
		node.Start(t)
	}

	n1 := nodes[0]
	// Start RBC
	msg := n1.rbc.RBroadcast(s)
	msgBytes, err := n1.rbc.Marshal(msg)
	require.NoError(t, err)
	err = n1.iface.Broadcast(msgBytes)
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
