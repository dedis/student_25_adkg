package avss

import (
	"context"
	"student_25_adkg/networking"
	"student_25_adkg/secretsharing"
	test "student_25_adkg/testing"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/group/edwards25519"
)

var defaultThreshold = 2

func getDefaultConfig() secretsharing.Config {
	g := edwards25519.NewBlakeSHA256Ed25519()
	offset := g.Scalar().Pick(g.RandomStream())
	g0 := g.Point().Base()
	g1 := g0.Mul(offset, g0)

	return secretsharing.Config{
		Group:     g,
		Base0:     g0,
		Base1:     g1,
		Threshold: defaultThreshold,
		NbNodes:   3*defaultThreshold + 1,
	}
}

type TestNode struct {
	iface networking.NetworkInterface
	avss  *AVSS
	rbc   *test.MockRBC
}

func NewTestNode(iface networking.NetworkInterface, conf secretsharing.Config, nodeID int64,
	mockRbc *test.MockRBC) *TestNode {
	avss := NewAVSS(conf, nodeID, iface, mockRbc)
	mockRbc.SetPredicate(avss.predicate)
	return &TestNode{
		iface: iface,
		avss:  avss,
		rbc:   mockRbc,
	}
}

func createNodes(interfacesAvss, interfacesRbc []networking.NetworkInterface,
	config secretsharing.Config) []*TestNode {
	nodes := make([]*TestNode, len(interfacesAvss))
	for i, iface := range interfacesAvss {
		rbcInstance := test.NewMockRBC(interfacesRbc[i], nil)
		nodes[i] = NewTestNode(iface, config, iface.GetID(), rbcInstance)
	}
	return nodes
}

func startNodes(ctx context.Context, nodes []*TestNode) {
	for _, node := range nodes {
		go func() {
			node.avss.Start(ctx)
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

	encodedCommitment, err := marshalCommitment(v)
	require.NoError(t, err)

	require.Equal(t, len(encodedCommitment), len(v)*g.PointLen())

	decodedCommitment, err := unmarshalCommitment(encodedCommitment, g)
	require.NoError(t, err)

	require.Equal(t, len(v), len(decodedCommitment))
	for i := 0; i < commitLength; i++ {
		require.True(t, v[i].Equal(decodedCommitment[i]))
	}
}

// TestAVSS_EndToEndSimple tests that starting an AVSS instance
func TestAVSS_EndToEndSimple(t *testing.T) {
	t.Skip("Skipping")
	conf := getDefaultConfig()
	ctx, cancel := context.WithCancel(context.Background())

	// Create separate networks for the RBC and AVSS communications
	interfacesAvss, err := test.SetupNetwork(networking.NewFakeNetwork(), conf.NbNodes)
	require.NoError(t, err)
	interfacesRbc, err := test.SetupNetwork(networking.NewFakeNetwork(), conf.NbNodes)
	require.NoError(t, err)

	nodes := createNodes(interfacesAvss, interfacesRbc, conf)

	startNodes(ctx, nodes)

	// Start AVSS
	secret := conf.Group.Scalar().SetInt64(int64(1))
	dealer := nodes[0]
	err = dealer.avss.Share(secret)
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	for _, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-node.avss.GetFinishedChannel()
		}()
	}

	// Wait for all nodes to finish
	wg.Wait()

	// Check the results
	for _, node := range nodes {
		result := node.avss.result
		require.True(t, secret.Equal(result))
	}

	cancel()
}
