package bracha

import (
	"context"
	"github.com/stretchr/testify/require"
	"student_25_adkg/networking"
	"sync"
	"testing"
	"time"
)

type TestNode struct {
	iface *MockAuthStream
	rbc   *BrachaRBC
}

// MockAuthStream mocks an authenticated message stream. Nothing is actually authenticated.
type MockAuthStream struct {
	Network    networking.NetworkInterface[[]byte]
	rcvChan    <-chan []byte
	readDelay  time.Duration
	writeDelay time.Duration
}

func NoDelayMockAuthStream(iface networking.NetworkInterface[[]byte]) *MockAuthStream {
	return &MockAuthStream{
		Network:    iface,
		rcvChan:    make(chan []byte),
		readDelay:  0,
		writeDelay: 0,
	}
}

func (iface *MockAuthStream) Broadcast(bytes []byte) error {
	// Artificially delay the broadcast
	time.Sleep(iface.writeDelay)
	return iface.Network.Broadcast(bytes)
}

func (iface *MockAuthStream) Receive(ctx context.Context) ([]byte, error) {
	msg, err := iface.Network.Receive(ctx)
	time.Sleep(iface.readDelay) // Artificially delay receiving
	return msg, err
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
func TestBrachaRBC_Simple(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()
	threshold := 1
	nbNodes := 3

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		stream := NoDelayMockAuthStream(network.JoinNetwork())
		node := NewTestNode(stream, NewBrachaRBC(pred, threshold, stream, uint32(i)))
		nodes[i] = node
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create a wait group to wait for all bracha instances to finish
	wg := sync.WaitGroup{}
	n1 := nodes[0]
	for i := 1; i < nbNodes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := nodes[i].rbc.Listen(ctx)
			if err != nil {
				// Log
				t.Logf("Error listening: %v", err)
			}
			t.Logf("Node %d done", i)
		}()
	}
	// Start RBC
	err := n1.rbc.RBroadcast(ctx, true)
	t.Log("Broadcast complete")
	require.NoError(t, err)

	wg.Wait()
	// Check that all nodes settled on the same correct value and all finished
	for _, n := range nodes {
		val := n.rbc.value
		finished := n.rbc.finished
		require.True(t, finished)
		require.True(t, val) // The value sent is True
	}

	cancel()
}
