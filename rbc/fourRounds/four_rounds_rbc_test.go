package fourRounds

import (
	"crypto/sha256"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"student_25_adkg/networking"
	"sync"
	"testing"
)

type TestNode struct {
	g     kyber.Group
	iface *MockAuthStream
	rbc   *FourRoundRBC[kyber.Scalar]
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

func (iface *MockAuthStream) Start(t *testing.T) {
	go func() {
		for {
			msg, err := iface.Network.Receive()
			if err != nil {
				t.Logf("Error receiving message: %v", err)
			}

			for _, handler := range iface.handlers {
				err = (*handler)(msg)
				if err != nil {
					t.Logf("Error handling message: %v", err)
				}
			}
		}
	}()
}

func NewTestNode(iface *MockAuthStream, rbc *FourRoundRBC[kyber.Scalar]) *TestNode {
	return &TestNode{
		iface: iface,
		rbc:   rbc,
	}
}

func pred([]kyber.Scalar) bool {
	// For now, we don't care what this does
	return true
}

type ScalarMarshaller struct {
	kyber.Group
}

func (sm *ScalarMarshaller) Marshal(s kyber.Scalar) ([]byte, error) {
	b, err := s.MarshalBinary()
	return b, err
}
func (sm *ScalarMarshaller) Unmarshal(b []byte) (kyber.Scalar, error) {
	s := sm.Group.Scalar().Zero()
	err := s.UnmarshalBinary(b)
	return s, err
}

// TestFourRoundsRBCSimple creates a network of 3 nodes with a threshold of 1 and then lets one node start dealing and waits
// sometime for the algorithm to finish and then check all nodes finished and settles on the same value that was dealt
func TestFourRoundsRBCSimple(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 1
	nbNodes := 4
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := 2 // Arbitrary message length
	s := make([]kyber.Scalar, mLen)
	for i := 0; i < mLen; i++ {
		s[i] = g.Scalar().Pick(g.RandomStream())
	}

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		stream := NewMockAuthStream(network.JoinNetwork())
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			nbNodes, 1, int32(i)))
		nodes[i] = node
		stream.Start(t)
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
		val := n.rbc.finalValue
		finished := n.rbc.finished
		require.True(t, finished)
		require.True(t, len(s) == len(val))
		for i := 0; i < len(val); i++ {
			require.True(t, val[i] == s[i])
		}
	}
}
