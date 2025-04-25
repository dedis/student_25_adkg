package networking

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Simple test to ensure a node joining works correctly
func Test_fake_network_join(t *testing.T) {
	network := NewFakeNetwork[[]byte]()

	nbNodes := 10
	expSize := 0
	// Test adding new nodes and check that they are being added
	for i := 0; i < nbNodes; i++ {
		node := network.JoinNetwork()
		expSize++
		// Check the list of nodes is updated
		require.Equal(t, len(network.nodes), expSize)
		// Check that the given queue is updated
		require.Equal(t, network.nodes[node.id], node.rcvQueue)
	}
}

// Test that sending and receiving a message between two nodes works
func Test_fake_network_Send_Receive(t *testing.T) {
	network := NewFakeNetwork[[]byte]()
	ctx, cancel := context.WithCancel(context.Background())

	n1 := network.JoinNetwork()
	n2 := network.JoinNetwork()

	msg := []byte("hello world")

	err := n1.Send(msg, n2.id)
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	received, err := n2.Receive(ctx)
	require.NoError(t, err)
	require.Equal(t, msg, received)
	cancel()
}

// Test a broadcast works correctly
func Test_fake_network_Send_Broadcast(t *testing.T) {
	network := NewFakeNetwork[[]byte]()
	ctx, cancel := context.WithCancel(context.Background())
	nbNodes := 10

	nodes := make([]*FakeInterface[[]byte], nbNodes)
	for i := 0; i < nbNodes; i++ {
		node := network.JoinNetwork()
		nodes[i] = node
	}

	n1 := nodes[0]

	msg := []byte("hello world")
	err := n1.Broadcast(msg)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// Check that everyone received the message (incl. n1)
	for _, node := range nodes {
		received, err := node.Receive(ctx)
		require.NotNil(t, received, "Node %d didn't receive a message", node.id)
		require.NoError(t, err)
		require.Equal(t, msg, received, "Node %d didn't received the right message. Got %s", node.id)
	}
	cancel()
}
