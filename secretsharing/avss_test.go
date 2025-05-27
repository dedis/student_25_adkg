package secretsharing

import (
	"context"
	"crypto/sha256"
	"errors"
	"student_25_adkg/logging"
	"student_25_adkg/networking"
	"student_25_adkg/rbc"
	"student_25_adkg/rbc/fourrounds"
	"student_25_adkg/reedsolomon"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/group/edwards25519"
)

var defaultThreshold = 2

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

type Instance struct {
	finished bool
	success  bool
	value    []byte
	hash     []byte
	sync.RWMutex
}

func NewInstance(messageHash []byte) *Instance {
	return &Instance{
		hash: messageHash,
	}
}

func (i *Instance) Identifier() []byte {
	i.RLock()
	defer i.RUnlock()
	return i.hash
}

func (i *Instance) GetValue() []byte {
	i.RLock()
	defer i.RUnlock()
	return i.value
}

func (i *Instance) Finished() bool {
	i.RLock()
	defer i.RUnlock()
	return i.finished
}

func (i *Instance) Success() bool {
	i.RLock()
	defer i.RUnlock()
	return i.success
}

func (i *Instance) Finish(value []byte) bool {
	i.Lock()
	defer i.Unlock()
	if i.finished {
		return false
	}
	i.finished = true
	i.value = value
	i.success = true
	return true
}

type MockRBC struct {
	predicate    func([]byte) bool
	finishedChan chan rbc.Instance[[]byte]
	iface        networking.NetworkInterface
	states       map[string]*Instance
	logger       zerolog.Logger
	sync.RWMutex
}

func NewMockRBC(iface networking.NetworkInterface, predicate func([]byte) bool) *MockRBC {
	return &MockRBC{
		predicate:    predicate,
		finishedChan: make(chan rbc.Instance[[]byte]),
		iface:        iface,
		states:       make(map[string]*Instance),
		logger:       logging.GetLogger(iface.GetID()),
	}
}

func (m *MockRBC) RBroadcast(msg []byte) error {
	ok := m.predicate(msg)
	if !ok {
		return rbc.ErrPredicateRejected
	}

	return m.iface.Broadcast(msg)
}

func (m *MockRBC) Listen(ctx context.Context) error {
	for {
		msg, err := m.iface.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			continue
		}
		m.logger.Info().Msg("Finished instance")

		m.Lock()
		hasher := sha256.New()
		hasher.Write(msg)
		hash := hasher.Sum(nil)

		state, ok := m.states[string(hash)]
		if !ok {
			state = NewInstance(hash)
			m.states[string(hash)] = state
		}

		_ = state.Finish(msg)
		m.finishedChan <- state
		m.Unlock()
	}
}

func (m *MockRBC) GetFinishedChannel() <-chan rbc.Instance[[]byte] {
	return m.finishedChan
}

type TestNode struct {
	iface networking.NetworkInterface
	avss  *AVSS
	rbc   *fourrounds.FourRoundRBC
}

func NewTestNode(iface networking.NetworkInterface, conf Config, nodeID int64, rbc *fourrounds.FourRoundRBC) *TestNode {
	avss := NewAVSS(conf, nodeID, iface, rbc)
	rbc.SetPredicate(avss.predicate)
	return &TestNode{
		iface: iface,
		avss:  avss,
		rbc:   rbc,
	}
}

func setupNetwork(nbNodes int) (networking.Network, []networking.NetworkInterface, error) {
	network := networking.NewFakeNetwork()

	nodes := make([]networking.NetworkInterface, nbNodes)
	for i := 0; i < nbNodes; i++ {
		node, err := network.JoinNetwork()
		if err != nil {
			return nil, nil, err
		}
		nodes[i] = node
	}

	return network, nodes, nil
}

func createNodes(network networking.Network, interfaces []networking.NetworkInterface, config Config) ([]*TestNode, error) {
	nodes := make([]*TestNode, len(interfaces))
	for i, iface := range interfaces {
		rs := reedsolomon.NewBWCodes(config.t+1, config.n)
		rbcIface, err := network.JoinNetwork()
		if err != nil {
			return nil, err
		}
		fourRoundsRBC := fourrounds.NewFourRoundRBC(nil, sha256.New(), config.t, rbcIface, rs, iface.GetID())
		nodes[i] = NewTestNode(iface, config, iface.GetID(), fourRoundsRBC)
	}
	return nodes, nil
}

func startNodes(ctx context.Context, nodes []*TestNode) {
	for _, node := range nodes {
		go func() {
			node.avss.start(ctx)
		}()
	}
}

func TestAVSS_EncodeDecodeCommitment(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	commitLength := 3
	v := make([]kyber.Point, commitLength)
	for i := 0; i < commitLength; i++ {
		v[i] = g.Point().Pick(g.RandomStream())
	}

	encodedCommitment, err := encodeCommitment(v)
	require.NoError(t, err)

	require.Equal(t, len(encodedCommitment), len(v)*g.PointLen())

	decodedCommitment, err := decodeCommitment(encodedCommitment, g)
	require.NoError(t, err)

	require.Equal(t, len(v), len(decodedCommitment))
	for i := 0; i < commitLength; i++ {
		require.True(t, v[i].Equal(decodedCommitment[i]))
	}
}

// TestAVSS_EndToEndSimple tests that starting an AVSS instance
func TestAVSS_EndToEndSimple(t *testing.T) {
	conf := getDefaultConfig()
	ctx, cancel := context.WithCancel(context.Background())

	network, interfaces, err := setupNetwork(conf.n)
	require.NoError(t, err)

	nodes, err := createNodes(network, interfaces, conf)
	require.NoError(t, err)

	startNodes(ctx, nodes)

	wg := sync.WaitGroup{}
	for _, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startErr := node.avss.Start(ctx)
			require.NoError(t, startErr)
		}()
	}

	// Start AVSS for all nodes
	for i, node := range nodes {
		secret := conf.g.Scalar().SetInt64(int64(i))
		shareErr := node.avss.Share(ctx, secret)
		require.NoError(t, shareErr)
	}

	for i, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-node.avss.GetFinishedChannel()
			expected := conf.g.Scalar().SetInt64(int64(i))
			result := node.avss.result
			require.True(t, expected.Equal(result))
		}()
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
