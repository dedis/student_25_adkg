package fourrounds

import (
	"bytes"
	"context"
	"crypto/sha256"
	"math/rand"
	"student_25_adkg/networking"
	"student_25_adkg/rbc/fourrounds/typedefs"
	"student_25_adkg/reedsolomon"
	"student_25_adkg/transport/udp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type TestNode struct {
	rbc *FourRoundRBC
	networking.NetworkInterface
}

func NewTestNode(iface networking.NetworkInterface, rbc *FourRoundRBC) *TestNode {
	return &TestNode{
		rbc:              rbc,
		NetworkInterface: iface,
	}
}

func startDummyNode(ctx context.Context, iface networking.NetworkInterface) {
	go func() {
		for {
			_, _ = iface.Receive(ctx)
			if ctx.Err() != nil {
				return
			}
		}
	}()
}

// defaultPredicate always return true. In RBC, the predicate simply
// allows to check that the message being broadcasted follow a given
// predicate but has nothing to do with the logic of the protocol other
// that if the predicate is not satisfied, the broadcast should be stopped
func defaultPredicate([]byte) bool {
	// For now, we don't care what this does
	return true
}

func createTestNodeWithDelay(network *networking.FakeNetwork, threshold, r, nbNodes,
	mLen int, delay time.Duration) *TestNode {
	nIface, err := network.JoinNetwork()
	if err != nil {
		panic(err)
	}
	network.DelayNode(nIface.GetID(), delay)
	rs := reedsolomon.NewBWCodes(mLen, nbNodes)
	node := NewTestNode(nIface, NewFourRoundRBC(defaultPredicate, sha256.New(), threshold, nIface, rs, r, nIface.GetID()))
	return node
}

func createDefaultNetworkTestNode(network networking.Network, mLen int) *TestNode {
	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nIface, err := network.JoinNetwork()
	if err != nil {
		panic(err)
	}
	rs := reedsolomon.NewBWCodes(mLen, nbNodes)
	node := NewTestNode(nIface, NewFourRoundRBC(defaultPredicate, sha256.New(), threshold, nIface, rs, r, nIface.GetID()))
	return node
}

func generateMessage(l int) []byte {
	r := rand.New(rand.NewSource(99))
	s := make([]byte, l)
	for i := 0; i < l; i++ {
		s[i] = byte(r.Intn(256))
	}
	return s
}

/****************************************************************************************/
/************************************** Unit Tests **************************************/
/****************************************************************************************/

// TestFourRoundsRBC_FreshHash test the FreshHash method. Create a random message to hash and test
// if hashing to times in a row creates the same hash
func TestFourRoundsRBC_FreshHash(t *testing.T) {
	// Set up a fake network
	network := networking.NewFakeNetwork()

	// Randomly generate a message to hash
	mLen := 3 // Length of the messages being sent
	s := generateMessage(mLen)

	node := createDefaultNetworkTestNode(network, mLen)

	h1, err := node.rbc.FreshHash(s)
	require.NoError(t, err)

	h2, err := node.rbc.FreshHash(s)
	require.NoError(t, err)

	require.Equal(t, h1, h2)
}

// TestFourRoundsRBC_Receive_Propose tests that a node correctly handles the reception of a PROPOSE message
func TestFourRoundsRBC_Receive_Propose(t *testing.T) {

	// Set up a fake network
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	// Config
	threshold := 2
	nbNodes := 3*threshold + 1
	mLen := threshold + 1 // Length of the messages being sent

	// Set up a node to test
	node := createDefaultNetworkTestNode(network, mLen)
	go func() {
		err := node.rbc.Listen(ctx)
		require.Error(t, context.Canceled, err)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	interfaces := make([]*networking.FakeInterface, nbNodes-1)
	for i := 0; i < nbNodes-1; i++ {
		iface, err := network.JoinNetwork()
		require.NoError(t, err)
		casted, ok := iface.(*networking.FakeInterface)
		require.True(t, ok)
		interfaces[i] = casted
		startDummyNode(ctx, casted)
	}

	// Send a PROPOSE message to the test node and check that it answers correctly
	s := generateMessage(mLen)
	sHash, err := node.rbc.FreshHash(s)
	require.NoError(t, err)
	proposeMessage := createProposeMessage(s)
	proposeBytes, err := proto.Marshal(proposeMessage)
	if err != nil {
		panic(err)
	}

	err = interfaces[1].Send(proposeBytes, node.GetID())
	require.NoError(t, err)
	t.Logf("Sent PROPOSE message to %d", node.GetID())

	// Wait a second for message to have been sent
	time.Sleep(100 * time.Millisecond)

	// Expect each interface to have received an echo message for each chunk
	for i, iface := range interfaces {
		sent := iface.GetSent()

		if i == 1 {
			require.Equal(t, 1, len(sent))
		} else {
			// Expect that no interface sent anything
			require.Equal(t, 0, len(sent))
		}

		received := iface.GetReceived()

		// The node who received the PROPOSE should have sent an ECHO message for to each node (but all broadcast i.e.
		// each node received nbNodes ECHO messages
		require.Equal(t, nbNodes, len(received))

		// Check that the messages received are ECHO and that the hash matches
		for j := 0; j < len(received); j++ {
			bs := received[j]
			msg := &typedefs.Instruction{}
			err = proto.Unmarshal(bs, msg)
			require.NoError(t, err)
			switch op := msg.Operation.Op.(type) {
			case *typedefs.Message_EchoInst:
				require.Equal(t, int64(j), op.EchoInst.GetIndex())
				require.True(t, bytes.Equal(op.EchoInst.GetMessageHash(), sHash))
			default:
				require.Fail(t, "Unexpected message type: %T", msg)
			}
		}
	}

	// Check the messages sent and received by the real node
	nSent := node.GetSent()

	// Node should have sent a broadcast to each node
	require.Equal(t, nbNodes, len(nSent))

	nReceived := node.GetReceived()
	// Node should have received the PROPOSE message and its own broadcasts
	require.Equal(t, 1+nbNodes, len(nReceived))

	// Try to reconstruct from the encoded messages
	messages := interfaces[0].GetReceived()
	chunks := make([]reedsolomon.Encoding, nbNodes)
	for i := 0; i < len(messages); i++ {
		bs := messages[i]
		msg := &typedefs.Instruction{}
		err = proto.Unmarshal(bs, msg)
		require.NoError(t, err)
		switch op := msg.GetOperation().GetOp().(type) {
		case *typedefs.Message_EchoInst:
			chunks[op.EchoInst.GetIndex()] = reedsolomon.Encoding{
				Idx: op.EchoInst.GetIndex(),
				Val: op.EchoInst.GetEncodingShare(),
			}
		default:
			require.Fail(t, "Unexpected message type: %T", msg)
		}
	}

	require.Equal(t, nbNodes, len(chunks))
	decoded, err := node.rbc.rs.Decode(chunks)
	require.NoError(t, err)

	require.Equal(t, len(s), len(decoded))
	// Check that the bytes of the decoded message match the original message sent
	require.True(t, bytes.Equal(s, decoded))

	// Stop all listening networks
	cancel()
}

// TestFourRoundsRBC_Receive_Echo checks that a node correctly handles receiving ECHO messages
// Send 2t+1 ECHO messages checking that nothing happens before the 2t+1-th is received and
// then a READY is sent only once
func TestFourRoundsRBC_Receive_Echo(t *testing.T) {
	// Set up a fake network
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	// Config
	threshold := 2
	nbNodes := 3*threshold + 1

	// Set up a node to test
	node := createDefaultNetworkTestNode(network, 4)
	go func() {
		err := node.rbc.Listen(ctx)
		require.Error(t, context.Canceled, err)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	interfaces := make([]*networking.FakeInterface, nbNodes-1)
	for i := 0; i < nbNodes-1; i++ {
		iface, err := network.JoinNetwork()
		require.NoError(t, err)
		casted, ok := iface.(*networking.FakeInterface)
		require.True(t, ok)
		interfaces[i] = casted
		// Start the interface to just receive messages and do nothing with them
		startDummyNode(ctx, casted)
	}

	fakeMi := []byte{1, 2, 3, 4} // Arbitrary
	hash := []byte{5, 6, 7, 8}
	echoMsg := createEchoMessage(fakeMi, hash, node.rbc.nodeID)
	echoBytes, err := proto.Marshal(echoMsg)
	require.NoError(t, err)

	echoThreshold := 2*threshold + 1
	sent := 0
	for i := 0; i < echoThreshold-1; i++ {
		// Send the ECHO message to the real node
		err := interfaces[0].Send(echoBytes, node.GetID())
		require.NoError(t, err)
		sent++
		// Wait a few milliseconds to make sure the node received and processed the message
		time.Sleep(10 * time.Millisecond)

		nReceived := node.GetReceived()
		// Should have received every message
		require.Equal(t, sent, len(nReceived))
		nSent := node.GetSent()
		// Should not have sent anything yet
		require.Equal(t, 0, len(nSent))
	}

	// Sent an ECHO message for another share of the encoding and expect nothing to happen
	echoMsg2 := createEchoMessage(fakeMi, hash, interfaces[0].GetID())
	echoBytes2, err := proto.Marshal(echoMsg2)
	require.NoError(t, err)
	err = interfaces[0].Send(echoBytes2, node.GetID())
	require.NoError(t, err)
	sent++

	time.Sleep(10 * time.Millisecond)

	nReceived := node.GetReceived()
	// Should have received all messages
	require.Equal(t, sent, len(nReceived))
	nSent := node.GetSent()
	// Should have sent nothing yet
	require.Equal(t, 0, len(nSent))

	// Sent another echo and expect a ready message to be broadcast
	err = interfaces[0].Send(echoBytes, node.GetID())
	require.NoError(t, err)
	sent++
	// Wait sometime to leave time for the node to have sent all its messages
	time.Sleep(10 * time.Millisecond)

	nReceived = node.GetReceived()
	// Should have received every ECHO message plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent = node.GetSent()
	// Should have sent a READY broadcast
	require.Equal(t, 1, len(nSent))

	// All other interfaces should have received one
	for _, iface := range interfaces {
		received := iface.GetReceived()
		require.Equal(t, 1, len(received))
	}

	// Send another ECHO and check that no other message is sent by the node
	err = interfaces[0].Send(echoBytes, node.GetID())
	require.NoError(t, err)
	sent++

	time.Sleep(10 * time.Millisecond)
	nReceived = node.GetReceived()
	// Should have received all messages plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent = node.GetSent()
	// Should only have sent a single ECHO message
	require.Equal(t, 1, len(nSent))
	for _, iface := range interfaces {
		received := iface.GetReceived()
		// Should only have received one ECHO from the node
		require.Equal(t, 1, len(received))
	}

	// Stop all
	cancel()
}

// TestFourRoundsRBC_Receive_Ready_before checks that a node correctly handles receiving READY messages
// Send t ready messages and expect nothing to be sent back. Then send t+1-th message
// and expect nothing until t+1 ECHO message for its corresponding share of the
// encoding is received
func TestFourRoundsRBC_Receive_Ready_before(t *testing.T) {
	// Set up a fake network
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	// Config
	threshold := 2
	nbNodes := 3*threshold + 1

	// Set up a node to test
	node := createDefaultNetworkTestNode(network, 4)
	go func() {
		err := node.rbc.Listen(ctx)
		require.Error(t, context.Canceled, err)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	interfaces := make([]*networking.FakeInterface, nbNodes-1)
	for i := 0; i < nbNodes-1; i++ {
		iface, err := network.JoinNetwork()
		require.NoError(t, err)
		casted, ok := iface.(*networking.FakeInterface)
		require.True(t, ok)
		interfaces[i] = casted
		// Start the interface to just receive messages and do nothing with them
		startDummyNode(ctx, casted)
	}

	fakeMi := []byte{1, 2, 3, 4} // Arbitrary
	hash := []byte{5, 6, 7, 8}
	readyMsg := createReadyMessage(fakeMi, hash, node.GetID())
	readyBytes, err := proto.Marshal(readyMsg)
	require.NoError(t, err)

	readyThreshold := threshold + 1

	// Send t READY messages and expect nothing each time
	sent := 0
	for i := 0; i < readyThreshold-1; i++ {
		err := interfaces[0].Send(readyBytes, node.GetID())
		require.NoError(t, err)
		sent++

		time.Sleep(10 * time.Millisecond)

		nReceived := node.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := node.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	// Send t+1 message and expect nothing
	err = interfaces[0].Send(readyBytes, node.GetID())
	require.NoError(t, err)
	sent++

	time.Sleep(10 * time.Millisecond)

	nReceived := node.GetReceived()
	require.Equal(t, sent, len(nReceived))

	nSent := node.GetSent()
	require.Equal(t, 0, len(nSent))

	for _, iface := range interfaces {
		received := iface.GetReceived()
		// Should not have received anything yet
		require.Equal(t, 0, len(received))
	}

	// Send t ECHO messages for the node and nothing should happen
	echoMsg := createEchoMessage(fakeMi, hash, node.GetID())
	echoBytes, err := proto.Marshal(echoMsg)
	require.NoError(t, err)

	echoThreshold := threshold + 1
	for i := 0; i < echoThreshold-1; i++ {
		err := interfaces[0].Send(echoBytes, node.GetID())
		require.NoError(t, err)
		sent++

		time.Sleep(10 * time.Millisecond)

		nReceived := node.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := node.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	// Sent t+1 and t+2 ECHO and the node should have sent one READY message
	err = interfaces[0].Send(echoBytes, node.GetID())
	require.NoError(t, err)
	err = interfaces[0].Send(echoBytes, node.GetID())
	require.NoError(t, err)
	sent += 2

	time.Sleep(10 * time.Millisecond)

	nReceived = node.GetReceived()
	// Should have received all messages plus its own READY broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent = node.GetSent()
	// Should have sent a READY broadcast
	require.Equal(t, 1, len(nSent))

	for _, iface := range interfaces {
		received := iface.GetReceived()
		// Should have received a READY broadcast from the node
		require.Equal(t, 1, len(received))
	}

	// Stop all
	cancel()
}

// TestFourRoundsRBC_Receive_Ready_after does a  similar test to TestRBC_Receive_Ready_before but
// here the node receives the ECHO messages before receiving the t+1 ready message
func TestFourRoundsRBC_Receive_Ready_after(t *testing.T) {
	// Set up a fake network
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	// Config
	threshold := 2
	nbNodes := 3*threshold + 1

	// Set up a node to test
	node := createDefaultNetworkTestNode(network, 4)
	go func() {
		err := node.rbc.Listen(ctx)
		require.Error(t, context.Canceled, err)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	interfaces := make([]*networking.FakeInterface, nbNodes-1)
	for i := 0; i < nbNodes-1; i++ {
		iface, err := network.JoinNetwork()
		require.NoError(t, err)
		casted, ok := iface.(*networking.FakeInterface)
		require.True(t, ok)
		interfaces[i] = casted
		// Start the interface to just receive messages and do nothing with them
		startDummyNode(ctx, casted)
	}

	// Create a READY message
	fakeMi := []byte{1, 2, 3, 4} // Arbitrary
	hash := []byte{5, 6, 7, 8}   // Arbitrary
	readyMsg := createReadyMessage(fakeMi, hash, node.GetID())
	readyBytes, err := proto.Marshal(readyMsg)
	require.NoError(t, err)

	// Create an ECHO message
	echoMsg := createEchoMessage(fakeMi, hash, node.GetID())
	echoBytes, err := proto.Marshal(echoMsg)
	require.NoError(t, err)

	// Sent t+1 ECHO and the node should do nothing (since no READY message has been sent, the
	// threshold for ECHO is 2t+1)
	echoThreshold := threshold + 1
	sent := 0
	for i := 0; i < echoThreshold; i++ {
		err := interfaces[0].Send(echoBytes, node.GetID())
		require.NoError(t, err)
		sent++

		time.Sleep(10 * time.Millisecond)

		nReceived := node.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := node.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	readyThreshold := threshold + 1
	// Send t READY messages and expect nothing each time
	for i := 0; i < readyThreshold-1; i++ {
		err := interfaces[0].Send(readyBytes, node.GetID())
		require.NoError(t, err)
		sent++

		time.Sleep(10 * time.Millisecond)

		nReceived := node.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := node.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	// Send the t+1 and t+2 READY message and expect one READY broadcast
	err = interfaces[0].Send(readyBytes, node.GetID())
	require.NoError(t, err)
	err = interfaces[0].Send(readyBytes, node.GetID())
	require.NoError(t, err)
	sent += 2

	time.Sleep(10 * time.Millisecond)

	nReceived := node.GetReceived()
	// Should have received all messages plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent := node.GetSent()
	// Should have sent one READY message
	require.Equal(t, 1, len(nSent))
	msg := &typedefs.Instruction{}
	err = proto.Unmarshal(nSent[0], msg)
	require.NoError(t, err)
	_, ok := msg.Operation.Op.(*typedefs.Message_ReadyInst)
	require.True(t, ok, "Message received should be a READY message")

	for _, iface := range interfaces {
		received := iface.GetReceived()
		// Should all have received a ready message
		require.Equal(t, 1, len(received))
		msg := &typedefs.Instruction{}
		err = proto.Unmarshal(received[0], msg)
		require.NoError(t, err)
		_, ok := msg.Operation.Op.(*typedefs.Message_ReadyInst)
		require.True(t, ok, "Message received should be a READY")

	}

	// Stop all
	cancel()
}

/***********************************************************************************************/
/************************************** Integration Tests **************************************/
/***********************************************************************************************/

// runBroadcast takes the node at index 0 from the given list of nodes and tells it to start RBC with the given
// message msg. All other nodes are set to listen. The method returns when the algorithm finished
// for all nodes
func runBroadcast(t *testing.T, nodes []*TestNode, nbNodes int, msg []byte) {
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
	err := n1.rbc.RBroadcast(ctx, msg)
	t.Log("Broadcast complete")
	require.NoError(t, err)

	wg.Wait()
	cancel()
}

// checkRBCResult checks that the state of the given node reflect a successful completion of
// an RBC algorithm given the reliably broadcast message msg. Doesn't return true of false but
// makes the testing fail in case of a problem
func checkRBCResult(t *testing.T, nodes []*TestNode, nbNodes int, msg []byte) {
	for i := 0; i < nbNodes; i++ {
		n := nodes[i]
		val := n.rbc.finalValue
		finished := n.rbc.finished
		require.True(t, finished)
		require.True(t, len(msg) == len(val))
		require.True(t, bytes.Equal(msg, val))
	}
}

func runAndCheckRBC(t *testing.T, nodes []*TestNode, nbNodes int, msg []byte) {
	runBroadcast(t, nodes, nbNodes, msg)
	checkRBCResult(t, nodes, nbNodes, msg)
}

// TestFourRoundsRBC_Simple creates a network with a threshold t=2 and n=3*t+1 nodes
// and start a broadcast from one node. Wait until the algorithm finishes for all nodes
// and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_Simple(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()

	threshold := 2
	nbNodes := 3*threshold + 1

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		nodes[i] = createDefaultNetworkTestNode(network, mLen)
	}

	// Run RBC and check the result
	runAndCheckRBC(t, nodes, nbNodes, s)
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// Wait until the algorithm finishes for all nodes and verifies that everyone agreed on the same value.
// In this situation, 1 node is dead, and we expect the algorithm to finish for all nodes alive correctly.
func TestFourRoundsRBC_OneDeadNode(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()

	threshold := 2
	nbNodes := 3*threshold + 1
	nbDead := 1

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the working nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes-nbDead; i++ {
		nodes[i] = createDefaultNetworkTestNode(network, mLen)
	}

	// Set up the dead nodes
	deadNodes := make([]networking.NetworkInterface, nbDead)
	for i := 0; i < nbDead; i++ {
		// Dead nodes just mean a node that joins the network but never receives or sends anything
		// i.e. creating a stream but never using it
		stream, err := network.JoinNetwork()
		require.NoError(t, err)
		deadNodes[i] = stream
	}

	// Run and check RBC
	runAndCheckRBC(t, nodes, nbNodes-nbDead, s)
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// Wait until the algorithm finishes for all nodes and verifies that everyone agreed on the same value.
// In this situation, 2 nodes are dead, and we expect the algorithm to finish for all nodes alive correctly.
func TestFourRoundsRBC_TwoDeadNode(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()

	threshold := 2
	nbNodes := 3*threshold + 1
	nbDead := 2

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the working nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes-nbDead; i++ {
		nodes[i] = createDefaultNetworkTestNode(network, mLen)
	}

	// Set up the dead nodes
	deadNodes := make([]networking.NetworkInterface, nbDead)
	for i := 0; i < nbDead; i++ {
		// Dead nodes just mean a node that joins the network but never receives or sends anything
		// i.e. creating a stream but never using it
		stream, err := network.JoinNetwork()
		require.NoError(t, err)
		deadNodes[i] = stream
	}

	// Run and check RBC
	runAndCheckRBC(t, nodes, nbNodes-nbDead, s)
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// In this situation, 3 nodes are dead, and we expect the algorithm to never finish.
func TestFourRoundsRBC_ThreeDeadNode(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()

	timeout := time.Second * 5 // Wait five seconds
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	threshold := 2
	nbNodes := 3*threshold + 1
	nbDead := 3

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the working nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes-nbDead; i++ {
		nodes[i] = createDefaultNetworkTestNode(network, mLen)
	}

	// Set up the dead nodes
	deadNodes := make([]networking.NetworkInterface, nbDead)
	for i := 0; i < nbDead; i++ {
		// Dead nodes just mean a node that joins the network but never receives or sends anything
		// i.e. creating a stream but never using it
		stream, err := network.JoinNetwork()
		require.NoError(t, err)
		deadNodes[i] = stream
	}

	// Run RBC in a go routing and call cancel on the context when the method returns (i.e. when the algorithm ends)
	go func() {
		runBroadcast(t, nodes, nbNodes-nbDead, s)
		cancel()
	}()

	// Wait for the context
	<-ctx.Done()

	// Require the deadline to have exceeded
	require.Equal(t, ctx.Err(), context.DeadlineExceeded)
}

// TestFourRoundsRBC_SlowNode creates a network with a threshold t=2 and n=3*t+1
// nodes and start a broadcast from one node. In this situation, 1 node is slow
// i.e. it has some delay when reading and writing to the network. The broadcast
// should still work fine, only slowly.
func TestFourRoundsRBC_SlowNode(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbSlow := 1

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		if i >= nbNodes-nbSlow {
			// Set up the slow nodes with half a second of delay when sending packets
			delay := time.Millisecond * 500
			nodes[i] = createTestNodeWithDelay(network, threshold, r, nbNodes, mLen, delay)
		} else {
			// All other nodes don't have delay
			nodes[i] = createDefaultNetworkTestNode(network, mLen)
		}
	}

	// Run and check RBC, it should work normally only slower
	runAndCheckRBC(t, nodes, nbNodes, s)
}

// TestFourRoundsRBC_SlowNodes creates a network with a threshold t=2 and n=3*t+1
// nodes and start a broadcast from one node. In this situation, 3 node are slow
// i.e. they have some delay when reading and writing to the network. The broadcast
// should still work fine, only slowly.
func TestFourRoundsRBC_SlowNodes(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbSlow := 3

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		if i > nbNodes-nbSlow {
			delay := time.Millisecond * 500
			nodes[i] = createTestNodeWithDelay(network, threshold, r, nbNodes, mLen, delay)
		} else {
			// All other nodes don't have delay
			nodes[i] = createDefaultNetworkTestNode(network, mLen)
		}
	}

	// Run and check RBC, it should work normally only slower
	runAndCheckRBC(t, nodes, nbNodes, s)
}

// TestFourRoundsRBC_SlowNodesStress create a network where all nodes
// are slow in their communication. Except the protocol to finish normally
// albeit slower
func TestFourRoundsRBC_SlowNodesStress(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()

	threshold := 4
	r := 3
	nbNodes := 3*threshold + 1
	delay := time.Millisecond * 200

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		nodes[i] = createTestNodeWithDelay(network, threshold, r, nbNodes, mLen, delay)
	}

	// Run and check RBC, it should work normally only slower
	runAndCheckRBC(t, nodes, nbNodes, s)
}

// TestFourRoundsRBC_DealAndDies creates a network with a threshold t=2 and n=3*t+1
// nodes and start a broadcast from one node. In this situation, the dealer deals
// and then dies immediately. We expect the algorithm to finish correctly for all other nodes.
func TestFourRoundsRBC_DealAndDies(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	threshold := 2
	nbNodes := 3*threshold + 1

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		nodes[i] = createDefaultNetworkTestNode(network, mLen)
	}

	// Set all nodes to listen except from one which will be the dead one
	// Create a wait group to wait for all instances to finish
	wg := sync.WaitGroup{}
	for i := 0; i < nbNodes-1; i++ {
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

	// Create a dying dealer. We don't need to actually create an RBC instance, we just need to send the PROPOSE
	// from some node in the network and never answer after that
	dyingDealer, err := network.JoinNetwork()
	require.NoError(t, err)

	// Start RBC from the dying dealer
	inst := createProposeMessage(s)
	out, err := proto.Marshal(inst)
	require.NoError(t, err)

	// Broadcast the initial PROPOSE message to start RBC
	err = dyingDealer.Broadcast(out)
	require.NoError(t, err)
	t.Log("Broadcast complete")

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodes, nbNodes-1, s)
	cancel()
}

// TestFourRoundsRBC_testStop tests that calling the Stop method on a node after having called the broadcast
// or listen function returns without error
func TestFourRoundsRBC_testStop(t *testing.T) {
	network := networking.NewFakeNetwork()

	// Randomly generate the value to broadcast
	mLen := 3 // Arbitrary message length
	s := generateMessage(mLen)

	node := createDefaultNetworkTestNode(network, mLen)

	// Listen on the node in a go routine and expect it to cancel the context before the timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)

	go func() {
		err := node.rbc.Listen(ctx)
		// Listen should have returned an ErrClosed since the stop function should have been called
		require.Error(t, context.Canceled, err)
		// If this is reached, the Listen method is returned so cancel the ctx to notify
	}()
	cancel()

	<-ctx.Done()
	require.Equal(t, context.Canceled, ctx.Err())

	// Similarly but with broadcast

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second*1)

	go func() {
		err := node.rbc.RBroadcast(ctx2, s)
		// Listen should have returned an ErrClosed since the stop function should have been called
		require.Error(t, context.Canceled, err)
		// If this is reached, the Listen method is returned so cancel the ctx to notify
	}()
	cancel2()

	<-ctx2.Done()
	require.Equal(t, context.Canceled, ctx2.Err())
}

// TestFourRoundsRBC_DealAndStop works similarly to TestFourRoundsRBC_DealAndDies but instead the Stop function
// is called on the dealer
func TestFourRoundsRBC_DealAndStop(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		// Add a bit of delay when reading to leave time for the dealer node to be stopped
		delay := time.Millisecond * 10
		nodes[i] = createTestNodeWithDelay(network, threshold, r, nbNodes, mLen, delay)
	}

	// Set all nodes to listen except from one which will be the dealer
	wg := sync.WaitGroup{}
	for i := 0; i < nbNodes-1; i++ {
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

	dealer := nodes[nbNodes-1]

	dealerCtx, dealerCancel := context.WithCancel(context.Background())
	go func() {
		// Start RBC from the dealer
		err := dealer.rbc.RBroadcast(dealerCtx, s)
		require.Error(t, context.Canceled, err)
		t.Log("Broadcast complete")

	}()

	// Stop the dealer
	time.Sleep(time.Millisecond * 10)
	dealerCancel()
	t.Log("Dealer Stopped")

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodes, nbNodes-1, s)

	// Check the dealer did not finish
	require.False(t, dealer.rbc.finished)
	cancel()
}

// TestFourRoundsRBC_ListenerDies works similarly to TestFourRoundsRBC_DealAndStop but instead a random node is
// stopped and not the dealer
func TestFourRoundsRBC_ListenerDies(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		// Add a bit of delay when reading to leave time for the dealer node to be stopped
		delay := time.Millisecond * 10
		nodes[i] = createTestNodeWithDelay(network, threshold, r, nbNodes, mLen, delay)
	}

	// Set all nodes to listen except from one which will be the dealer and the one that will fail
	wg := sync.WaitGroup{}
	for i := 1; i < nbNodes-1; i++ {
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

	// Start the failing node
	failingCtx, failingCancel := context.WithCancel(context.Background())
	wg.Add(1)
	go func() {
		defer wg.Done()
		failNode := nodes[nbNodes-1]
		err := failNode.rbc.Listen(failingCtx)
		require.Error(t, context.Canceled, err)
		t.Log("Failing node finished")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Start RBC from the dealer
		err := nodes[0].rbc.RBroadcast(ctx, s)
		require.NoError(t, err)
		t.Log("Broadcast complete")
	}()

	// Stop the failing node
	time.Sleep(time.Millisecond * 30)
	failingCancel()

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodes, nbNodes-1, s)

	// Check that the last node did not finish
	require.False(t, nodes[nbNodes-1].rbc.finished)
	cancel()
}

// TestFourRoundsRBC_ListenerDies works similarly to TestFourRoundsRBC_DealAndStop but instead two random nodes are
// stopped and not the dealer
func TestFourRoundsRBC_TwoListenerDies(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbFailing := 2

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		// Add a bit of delay when reading to leave time for the dealer node to be stopped
		delay := time.Millisecond * 10
		nodes[i] = createTestNodeWithDelay(network, threshold, r, nbNodes, mLen, delay)
	}

	// Set all nodes to listen except from one which will be the dealer and the one that will fail
	wg := sync.WaitGroup{}
	for i := 1; i < nbNodes-nbFailing; i++ {
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

	// Start the failing nodes
	failingCtx, failingCancel := context.WithCancel(context.Background())
	for i := 0; i < nbFailing; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failNode := nodes[nbNodes-i-1]
			err := failNode.rbc.Listen(failingCtx)
			require.Error(t, context.Canceled, err)
			t.Logf("Failing node %d finished", i)
		}()
	}

	// Stat dealing
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Start RBC from the dealer
		err := nodes[0].rbc.RBroadcast(ctx, s)
		require.NoError(t, err)
		t.Log("Broadcast complete")
	}()

	// Wait  little bit
	// Stop the failing nodes
	time.Sleep(time.Millisecond * 20)
	failingCancel()
	t.Log("Failing node finished")

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodes, nbNodes-nbFailing, s)

	// Check that the last node did not finish
	for i := 0; i < nbFailing; i++ {
		require.False(t, nodes[nbNodes-i-1].rbc.finished)
	}
	cancel()
}

// TestFourRoundsRBC_Stress creates a network with a threshold t=20 and n=3*t+1
// nodes and start a broadcast from one node. Wait until the algorithm finishes
// for all nodes and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_Stress(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()

	threshold := 20
	r := 3
	nbNodes := 3*threshold + 1

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		// Create a custom node with a special sized buffer
		iface, err := network.JoinWithBuffer(4000)
		require.NoError(t, err)
		rs := reedsolomon.NewBWCodes(mLen, nbNodes)
		node := NewTestNode(iface, NewFourRoundRBC(defaultPredicate, sha256.New(), threshold, iface,
			rs, r, iface.GetID()))
		nodes[i] = node
	}

	// Run RBC and check the result
	runAndCheckRBC(t, nodes, nbNodes, s)
}

// TestFourRoundsRBC_Simple creates a network with a threshold t=2 and n=3*t+1 nodes
// and start a broadcast from one node. Wait until the algorithm finishes for all nodes
// and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_RealNetwork(t *testing.T) {
	// Config
	network := networking.NewTransportNetwork(udp.NewUDP())

	threshold := 2
	nbNodes := 3*threshold + 1

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen)

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		nodes[i] = createDefaultNetworkTestNode(network, mLen)
	}

	// Run RBC and check the result
	runAndCheckRBC(t, nodes, nbNodes, s)
}
