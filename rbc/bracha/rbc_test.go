package bracha

import (
	"context"
	"student_25_adkg/networking"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type TestNode struct {
	iface networking.NetworkInterface
	rbc   *RBC
}

func NewTestNode(iface networking.NetworkInterface, rbc *RBC) *TestNode {
	return &TestNode{
		iface: iface,
		rbc:   rbc,
	}
}

// defaultPredicate always return true. In RBC, the predicate simply
// allows to check that the message being broadcasted follow a given
// predicate but has nothing to do with the logic of the protocol other
// that if the predicate is not satisfied, the broadcast should be stopped
func defaultPredicate(bool) bool {
	return true
}

func runRBCWithValue(t *testing.T, val bool) {
	// Config
	network := networking.NewFakeNetwork()
	threshold := 1
	nbNodes := 3

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		stream, err := network.JoinNetwork()
		require.NoError(t, err)
		node := NewTestNode(stream, NewBrachaRBC(defaultPredicate, threshold, stream, int64(i)))
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
	err := n1.rbc.RBroadcast(ctx, val)
	t.Log("Broadcast complete")
	require.NoError(t, err)

	wg.Wait()
	// Check that all nodes settled on the same correct value and all finished
	for _, n := range nodes {
		actual := n.rbc.value
		finished := n.rbc.finished
		require.True(t, finished)
		require.Equal(t, val, actual) // The value sent is True
	}

	cancel()
}

// TestBrachaSimple creates a network of 3 nodes with a threshold of 1 and then lets one node start dealing and waits
// sometime for the algorithm to finish and then check all nodes finished and settles on the same value that was dealt
func TestBrachaRBC_Simple(t *testing.T) {
	runRBCWithValue(t, true)
}

// TestBrachaSimple creates a network of 3 nodes with a threshold of 1 and then lets one node start dealing and waits
// sometime for the algorithm to finish and then check all nodes finished and settles on the same value that was dealt
func TestBrachaRBC_SimpleFalse(t *testing.T) {
	runRBCWithValue(t, false)
}
