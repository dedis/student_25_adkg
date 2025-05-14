package bracha

import (
	"context"
	"student_25_adkg/networking"
	"student_25_adkg/rbc"
	"sync"
	"testing"
	"time"

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

func setupNetwork(t require.TestingT, ctx context.Context, threshold int) []*TestNode {
	network := networking.NewFakeNetwork()
	nbNodes := 3*threshold + 1

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		stream := network.JoinNetwork()
		node := NewTestNode(stream, NewBrachaRBC(defaultPredicate, threshold, stream, int64(i)))
		nodes[i] = node
		go func() {
			err := node.rbc.Start(ctx)
			require.Error(t, err)
		}()
	}

	return nodes
}

func checkInstance(t require.TestingT, instance *Instance, expectedValue bool) {
	require.True(t, instance.IsFinished())
	result, err := instance.GetResult()
	require.NoError(t, err)
	require.Equal(t, expectedValue, result)
}

// runRBCWithValue calls broadcast on the list of dealers given by their index with the corresponding value in the
// in the values list and test for each instance that all nodes finished with the correct value
func runRBCWithValue(t require.TestingT, nodes []*TestNode, dealersIdx []int, values []bool) {
	// Create a wait group to wait for all bracha instances to finish
	wg := sync.WaitGroup{}

	// Start RBC from each dealer
	instances := make(map[rbc.InstanceIdentifier]bool)
	for _, i := range dealersIdx {
		expectedInstance := rbc.InstanceIdentifier(nodes[i].rbc.GetIndex())
		value := values[i]
		instances[expectedInstance] = value

		instanceID, err := nodes[i].rbc.RBroadcast(value)
		require.NoError(t, err)
		require.Equal(t, expectedInstance, instanceID)
	}

	time.Sleep(10 * time.Millisecond)

	for instanceID, expectedValue := range instances {
		for i := 0; i < len(nodes); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				instance, err := nodes[i].rbc.GetInstance(instanceID)
				require.NoError(t, err)
				require.NotNil(t, instance)
				brachaInstance := instance.(*Instance)

				<-brachaInstance.getFinishedChan()

				checkInstance(t, brachaInstance, expectedValue)
			}()
		}

		wg.Wait()

	}
}

// TestBrachaSimple creates a network of 3 nodes with a threshold of 1 and then lets one node Start dealing and waits
// sometime for the algorithm to finish and then check all nodes finished and settles on the same value that was dealt
func TestBrachaRBC_SimpleTrue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	threshold := 1
	nodes := setupNetwork(t, ctx, threshold)
	runRBCWithValue(t, nodes, []int{0}, []bool{true})
	cancel()
}

// TestBrachaSimple creates a network of 3 nodes with a threshold of 1 and then lets one node Start dealing and waits
// sometime for the algorithm to finish and then check all nodes finished and settles on the same value that was dealt
func TestBrachaRBC_SimpleFalse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	threshold := 1
	nodes := setupNetwork(t, ctx, threshold)
	runRBCWithValue(t, nodes, []int{0}, []bool{false})
	cancel()
}

func TestBrachaRBC_MultipleInstances(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	threshold := 2
	nodes := setupNetwork(t, ctx, threshold)
	runRBCWithValue(t, nodes, []int{0, 1}, []bool{false, false})
	cancel()
}

func TestRBC_MediumNetwork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	threshold := 5
	nodes := setupNetwork(t, ctx, threshold)
	runRBCWithValue(t, nodes, []int{0, 1}, []bool{false, false})
	cancel()
}
