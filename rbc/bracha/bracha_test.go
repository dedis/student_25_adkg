package bracha

import (
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"student_25_adkg/networking"
	"sync"
	"testing"
)

type TestNode struct {
	g     kyber.Group
	iface *MockAuthStream
	rbc   *BrachaRBC
	stop  bool
}

type MockAuthStream struct {
	Network  networking.NetworkInterface[[]byte]
	handlers []*func([]byte) error
}

func NewMockAuthStream(iface networking.NetworkInterface[[]byte]) *MockAuthStream {
	return &MockAuthStream{
		Network: iface,
	}
}

func (iface *MockAuthStream) Broadcast(bytes []byte) error {
	return iface.Network.Broadcast(bytes)
}

func (iface *MockAuthStream) AddHandler(handler func([]byte) error) {
	iface.handlers = append(iface.handlers, &handler)
}

func (iface *MockAuthStream) Start() {
	go func() {
		for {
			msg, err := iface.Network.Receive()
			if err != nil {
				// Probably log
			}

			for _, handler := range iface.handlers {
				err = (*handler)(msg)
				if err != nil {
					// Probably log
				}
			}
		}
	}()
}

func NewTestNode(iface *MockAuthStream, rbc *BrachaRBC) *TestNode {
	return &TestNode{
		iface: iface,
		rbc:   rbc,
	}
}

func pred(bool) bool {
	return true
}

// TestBrachaSimple creates a network of 3 nodes with a threshold of 1 and then lets one node start dealing and waits
// sometime for the algorithm to finish and then check all nodes finished and settles on the same value that was dealt
func TestBrachaSimple(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()
	s := true // Arbitrary binary value
	threshold := 1
	nbNodes := 3

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		stream := NewMockAuthStream(network.JoinNetwork())
		node := NewTestNode(stream, NewBrachaRBC(pred, threshold, stream))
		nodes[i] = node
		stream.Start()
	}

	// Create a wait group to wait for all bracha instances to finish
	wg := sync.WaitGroup{}
	n1 := nodes[0]
	for i := 1; i < nbNodes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := nodes[i].rbc.Listen()
			if err != nil {
				// Log
				t.Logf("Error listening: %v", err)
			}
			t.Logf("Node %d done", i)
		}()
	}
	// Start RBC
	err := n1.rbc.RBroadcast(s)
	t.Log("Broadcast complete")
	require.NoError(t, err)

	wg.Wait()
	// Check that all nodes settled on the same correct value and all finished
	for _, n := range nodes {
		val := n.rbc.value
		finished := n.rbc.finished
		require.True(t, finished)
		require.True(t, s == val)
	}
}
