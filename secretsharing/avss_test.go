package secretsharing

import (
	"context"
	"student_25_adkg/networking"
	"student_25_adkg/rbc"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/group/edwards25519"
)

var defaultThreshold = 3

func getDefaultConfig() Config {
	g := edwards25519.NewBlakeSHA256Ed25519()
	offset := g.Scalar().Pick(g.RandomStream())
	g0 := g.Point().Base()
	g1 := g0.Mul(offset, g0)

	return Config{
		g:  g,
		g0: g0,
		g1: g1,
		t:  defaultThreshold,
		n:  3*defaultThreshold + 1,
	}
}

type MockRBC struct {
	predicate     func([]byte) bool
	consensusChan chan []byte
	finished      bool
	value         []byte
	broadcasted   []byte
}

func NewMockRBC(predicate func([]byte) bool) *MockRBC {
	return &MockRBC{
		predicate:     predicate,
		consensusChan: make(chan []byte),
	}
}

func (m *MockRBC) waitConsensus(ctx context.Context) error {
	m.consensusChan = make(chan []byte)

	// Wait for consensus to be reached
	select {
	case value := <-m.consensusChan:
		m.value = value
		m.finished = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *MockRBC) RBroadcast(ctx context.Context, msg []byte) error {
	ok := m.predicate(msg)
	if !ok {
		return rbc.ErrPredicateRejected
	}
	m.broadcasted = msg
	return m.waitConsensus(ctx)
}

func (m *MockRBC) Listen(ctx context.Context) error {
	return m.waitConsensus(ctx)
}

func (m *MockRBC) setConsensusReached(val []byte) {
	m.consensusChan <- val
}

type TestNode struct {
	iface networking.NetworkInterface
	avss  *AVSS
	rbc   *MockRBC
}

func NewTestNode(iface networking.NetworkInterface, conf Config, nodeID int64) *TestNode {
	mockRBC := NewMockRBC(func([]byte) bool { return true })
	avss := NewAVSS(conf, nodeID, iface, mockRBC)
	mockRBC.predicate = avss.predicate
	return &TestNode{
		iface: iface,
		avss:  NewAVSS(conf, nodeID, iface, mockRBC),
		rbc:   mockRBC,
	}
}

func setupNetwork(nbNodes int) (*networking.FakeNetwork, []networking.NetworkInterface) {
	network := networking.NewFakeNetwork()

	nodes := make([]networking.NetworkInterface, nbNodes)
	for i := 0; i < nbNodes; i++ {
		nodes[i] = network.JoinNetwork()
	}

	return network, nodes
}

// TestAVSS_EndToEndSimple tests that starting an AVSS instance
func TestAVSS_EndToEndSimple(t *testing.T) {
	conf := getDefaultConfig()
	ctx, cancel := context.WithCancel(context.Background())

	_, interfaces := setupNetwork(conf.n)

	secret := conf.g.Scalar().SetInt64(int64(5))

	wg := sync.WaitGroup{}
	nodes := make([]*TestNode, conf.n)
	for i, node := range interfaces {
		nodes[i] = NewTestNode(node, conf, int64(i+1))
		if i != 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, err := nodes[i].avss.Listen(ctx)
				require.NoError(t, err)
				require.True(t, s.Equal(secret))
			}()
		}
	}

	// Start AVSS
	err := nodes[0].avss.Share(ctx, secret)
	require.NoError(t, err)

	// Finish mock RBC on all nodes
	msg := nodes[0].rbc.broadcasted
	for _, n := range nodes {
		n.rbc.setConsensusReached(msg)
	}

	// Wait for all nodes to finish
	wg.Wait()
	cancel()
}

// TestAVSS_EndToEndDelay test the protocol in a settings where the network
// is slow. The protocol should still work although more slowly
func TestAVSS_EndToEndDelay(t *testing.T) {
	// TODO
}

// TestAVSS_EndToEndDyingNodes test the protocol in a setting where nodes in the
// network die. As long as a threshold amount of nodes are still alive, the
// protocol should still work
func TestAVSS_EndToEndDyingNodes(t *testing.T) {
	// TODO
}
