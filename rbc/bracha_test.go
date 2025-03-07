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
	rbc   *BrachaRBC
	stop  bool
}

func NewTestNode(iface networking.NetworkInterface[BrachaMsg], rbc *BrachaRBC) *TestNode {
	return &TestNode{
		iface: iface,
		rbc:   rbc,
	}
}

func (n *TestNode) HandleMessage(t *testing.T, received BrachaMsg) {
	t.Logf("[%d] Received message type: %d, content: %s", n.iface.GetID(), received.MsgType, received.MsgContent.String())
	switch received.MsgType {
	case Propose:
		echo := n.rbc.ReceivePropose(received.MsgContent)
		if echo {
			t.Logf("[%d] Sending echo message", n.iface.GetID())
			err := n.rbc.BroadcastEcho(received.MsgContent, n.iface)
			if err != nil {
				t.Logf("Error broadcasting echo msg: %s\n", err)
			}
		}
	case Echo:
		ready := n.rbc.ReceiveEcho(received.MsgContent)
		if ready {
			t.Logf("[%d] Sending Ready message", n.iface.GetID())
			err := n.rbc.BroadcastReady(received.MsgContent, n.iface)
			if err != nil {
				t.Logf("Error broadcasting ready msg: %s\n", err)
			}
		}
	case Ready:
		val, finished, ready := n.rbc.ReceiveReady(received.MsgContent)
		if finished {
			t.Logf("Protocol finished, settled on value: %s\n", val)
		} else if ready {
			t.Logf("[%d] Sending ready msg", n.iface.GetID())
			err := n.rbc.BroadcastReady(received.MsgContent, n.iface)
			if err != nil {
				t.Logf("Error broadcasting ready msg: %s\n", err)
			}
		}
	}
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
	err := n1.rbc.Broadcast(s, n1.iface)
	require.NoError(t, err)

	time.Sleep(10 * time.Second)
}
