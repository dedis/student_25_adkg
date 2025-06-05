package fourrounds

import (
	"bytes"
	"context"
	"crypto/sha256"
	"math/rand"
	"student_25_adkg/networking"
	"student_25_adkg/reedsolomon"
	"student_25_adkg/transport/udp"
	"student_25_adkg/typedefs"
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

func createDefaultNetworkTestNode(network networking.Network, mLen int) *TestNode {
	threshold := 2
	nbNodes := 3*threshold + 1
	nIface, err := network.JoinNetwork()
	if err != nil {
		panic(err)
	}
	rs := reedsolomon.NewBWCodes(mLen, nbNodes)
	node := NewTestNode(nIface, NewFourRoundRBC(sha256.New(), threshold, nIface, rs, nIface.GetID()))
	return node
}

// Generate a random message of the given size and return its hash along
func generateMessage(l int) ([]byte, []byte) {
	r := rand.New(rand.NewSource(99))
	s := make([]byte, l)
	for i := 0; i < l; i++ {
		s[i] = byte(r.Intn(256))
	}
	hash := sha256.New()
	hash.Write(s)
	hashed := hash.Sum(nil)
	return s, hashed
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
	s, _ := generateMessage(mLen)

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
	nbNodes := threshold*3 + 1

	// Set up a node to test
	node := createDefaultNetworkTestNode(network, threshold+1)
	go func() {
		err := node.rbc.Listen(ctx)
		// Should stop listening because the context was cancelled
		require.ErrorIs(t, err, context.Canceled)
	}()

	// Connect a "fake" node i.e. get the  interface without creating a test node since we just
	// want to see what it receives from the real node
	iface, err := network.JoinNetwork()
	require.NoError(t, err)
	startDummyNode(ctx, iface)

	// Send a PROPOSE message to the test node and check that it answers correctly
	message, hash := generateMessage(threshold + 1)
	proposeMessage := createProposeMessage(message)
	packet := &typedefs.Packet{
		Message: &typedefs.Packet_RbcMessage{
			RbcMessage: proposeMessage,
		},
	}
	proposeBytes, err := proto.Marshal(packet)
	require.NoError(t, err)

	err = iface.Send(proposeBytes, node.GetID())
	require.NoError(t, err)

	// Wait a second for message to have been sent
	time.Sleep(100 * time.Millisecond)

	received := iface.GetReceived()

	// Should have received one ECHO message from the node
	require.Equal(t, 1, len(received))

	// Check that the message received is an ECHO and that the hash matches
	packet = &typedefs.Packet{}
	err = proto.Unmarshal(received[0], packet)
	require.NoError(t, err)
	rbcMessage, ok := packet.GetMessage().(*typedefs.Packet_RbcMessage)
	require.True(t, ok)
	echoMessage, ok := rbcMessage.RbcMessage.GetOp().(*typedefs.RBCMessage_EchoInst)
	require.True(t, ok)
	messageHash := echoMessage.EchoInst.GetMessageHash()
	require.True(t, bytes.Equal(messageHash, hash))

	// Check the messages sent and received by the real node
	nSent := node.GetSent()

	// Node should have sent an ECHO broadcast
	require.Equal(t, 1, len(nSent))

	nReceived := node.GetReceived()
	// Node should have received the PROPOSE message and its own ECHO broadcast
	require.Equal(t, 2, len(nReceived))

	// Node should have a stored a new instance with the corresponding hash
	_, ok = node.rbc.GetState(hash)
	require.True(t, ok)

	// Try to reconstruct from the ECHO message
	// The ECHO message should have a share for each node
	require.Equal(t, nbNodes, len(echoMessage.EchoInst.GetEncodingShares()))

	chunks := make([]*reedsolomon.Encoding, nbNodes)
	for i := range echoMessage.EchoInst.GetEncodingShares() {
		index := echoMessage.EchoInst.GetSharesIndices()[i]
		chunks[index] = &reedsolomon.Encoding{
			Idx: index,
			Val: echoMessage.EchoInst.GetEncodingShares()[i],
		}
	}

	require.Equal(t, nbNodes, len(chunks))
	decoded, err := node.rbc.rs.Decode(chunks)
	require.NoError(t, err)

	require.Equal(t, len(message), len(decoded))
	// Check that the bytes of the decoded message match the original message sent
	require.True(t, bytes.Equal(message, decoded))

	// Stop all listening networks
	cancel()
}

// TestFourRoundsRBC_Receive_Echo checks that a node correctly handles receiving ECHO messages
// Send 2t+1 ECHO messages checking that nothing happens before the 2t+1-th is received and
// then a READY is sent only once
func TestFourRoundsRBC_Receive_Echo(t *testing.T) {
	// Set up a fake network
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewFakeNetwork()

	// Config
	threshold := 2

	// Set up a node to test
	node := createDefaultNetworkTestNode(network, threshold+1)
	go func() {
		err := node.rbc.Listen(ctx)
		// Should have stopped listening because the context was cancelled
		require.ErrorIs(t, err, context.Canceled)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	networkInterface, err := network.JoinNetwork()
	require.NoError(t, err)
	// Start the interface to just receive messages and do nothing with them
	startDummyNode(ctx, networkInterface)

	fakeShares := []*reedsolomon.Encoding{
		{
			Idx: node.rbc.nodeID,
			Val: []byte{1, 2, 3, 4},
		},
	}
	hash := []byte{5, 6, 7, 8}
	echoMsg := createEchoMessage(fakeShares, hash)
	packet := &typedefs.Packet{
		Message: &typedefs.Packet_RbcMessage{
			RbcMessage: echoMsg,
		},
	}
	echoBytes, err := proto.Marshal(packet)
	require.NoError(t, err)

	echoThreshold := 2*threshold + 1
	sent := 0
	for i := 0; i < echoThreshold-1; i++ {
		// Send the ECHO message to the real node
		err := networkInterface.Send(echoBytes, node.GetID())
		require.NoError(t, err)
		sent++
		// Wait to make sure the node received and processed the message
		time.Sleep(10 * time.Millisecond)

		nReceived := node.GetReceived()
		// Should have received every message
		require.Equal(t, sent, len(nReceived))
		nSent := node.GetSent()
		// Should not have sent anything yet
		require.Equal(t, 0, len(nSent))
	}

	time.Sleep(10 * time.Millisecond)

	// Send another ECHO message and expect a READY message to be broadcast
	err = networkInterface.Send(echoBytes, node.GetID())
	require.NoError(t, err)
	sent++

	// Wait to leave time for the node to have sent its messages
	time.Sleep(10 * time.Millisecond)

	nReceived := node.GetReceived()
	// Should have received every ECHO message plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent := node.GetSent()
	// Should have sent a READY broadcast
	require.Equal(t, 1, len(nSent))

	// The interface should have received the READY broadcast of the node
	received := networkInterface.GetReceived()
	require.Equal(t, 1, len(received))

	// Send another ECHO and check that no other message is sent by the node
	err = networkInterface.Send(echoBytes, node.GetID())
	require.NoError(t, err)
	sent++

	time.Sleep(10 * time.Millisecond)
	nReceived = node.GetReceived()
	// Should have received all messages plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent = node.GetSent()
	// The node should not have sent another READY message
	require.Equal(t, 1, len(nSent))
	received = networkInterface.GetReceived()
	// Should only have received one READY message from the node
	require.Equal(t, 1, len(received))

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
	node := createDefaultNetworkTestNode(network, threshold+1)
	go func() {
		err := node.rbc.Listen(ctx)
		// Should have stopped listening because the context was cancelled
		require.ErrorIs(t, err, context.Canceled)
	}()

	// Connect "fake" node i.e. get an interface without creating a test node since we just
	// want to see what the interface receive from the real node
	iface, err := network.JoinNetwork()
	require.NoError(t, err)
	// Start the interface to just receive messages and do nothing with them
	startDummyNode(ctx, iface)

	fakeShares := make([]*reedsolomon.Encoding, nbNodes)
	for i := 0; i < nbNodes; i++ {
		shareBytes := make([]byte, 4)
		for j := 0; j < 4; j++ {
			shareBytes[j] = byte(4*i + j)
		}
		fakeShares[i] = &reedsolomon.Encoding{
			Idx: int64(i),
			Val: shareBytes,
		}
	}
	hash := []byte{5, 6, 7, 8}

	readyThreshold := threshold + 1

	// Send t READY messages and expect nothing each time
	sent := 0
	sharedIdx := 0
	for i := 0; i < readyThreshold-1; i++ {
		readyMsg := createReadyMessage(fakeShares[sharedIdx].Val, hash, fakeShares[0].Idx)
		packet := &typedefs.Packet{
			Message: &typedefs.Packet_RbcMessage{
				RbcMessage: readyMsg,
			},
		}
		readyBytes, err := proto.Marshal(packet)
		require.NoError(t, err)
		err = iface.Send(readyBytes, node.GetID())
		require.NoError(t, err)
		sent++

		time.Sleep(10 * time.Millisecond)

		nReceived := node.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := node.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))

		sharedIdx++
	}

	// Send t+1 message and expect nothing
	readyMsg := createReadyMessage(fakeShares[sharedIdx].Val, hash, fakeShares[0].Idx)
	packet := &typedefs.Packet{
		Message: &typedefs.Packet_RbcMessage{
			RbcMessage: readyMsg,
		},
	}
	readyBytes, err := proto.Marshal(packet)
	require.NoError(t, err)
	err = iface.Send(readyBytes, node.GetID())
	require.NoError(t, err)
	sent++

	time.Sleep(10 * time.Millisecond)

	nReceived := node.GetReceived()
	require.Equal(t, sent, len(nReceived))

	nSent := node.GetSent()
	require.Equal(t, 0, len(nSent))

	received := iface.GetReceived()
	// Should not have received anything yet
	require.Equal(t, 0, len(received))

	// Send t ECHO messages for the node and nothing should happen
	echoMsg := createEchoMessage(fakeShares, hash)
	packet = &typedefs.Packet{
		Message: &typedefs.Packet_RbcMessage{
			RbcMessage: echoMsg,
		},
	}
	echoBytes, err := proto.Marshal(packet)
	require.NoError(t, err)

	echoThreshold := threshold + 1
	for i := 0; i < echoThreshold-1; i++ {
		err := iface.Send(echoBytes, node.GetID())
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

	// Send t+1 and t+2 ECHO and the node should have sent one READY message
	err = iface.Send(echoBytes, node.GetID())
	require.NoError(t, err)
	err = iface.Send(echoBytes, node.GetID())
	require.NoError(t, err)
	sent += 2

	time.Sleep(10 * time.Millisecond)

	nReceived = node.GetReceived()
	// Should have received all messages plus its own READY broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent = node.GetSent()
	// Should have sent a READY broadcast
	require.Equal(t, 1, len(nSent))

	received = iface.GetReceived()
	// Should have received a READY broadcast from the node
	require.Equal(t, 1, len(received))

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
	node := createDefaultNetworkTestNode(network, threshold+1)
	go func() {
		err := node.rbc.Listen(ctx)
		// Start the interface to just receive messages and do nothing with them
		require.ErrorIs(t, err, context.Canceled)
	}()

	// Connect "fake" node i.e. get an interface without creating a test node since we just
	// want to see what the interface receives from the real node
	iface, err := network.JoinNetwork()
	require.NoError(t, err)
	// Start the interface to just receive messages and do nothing with them
	startDummyNode(ctx, iface)

	// Create a fake shares
	fakeShares := make([]*reedsolomon.Encoding, nbNodes)
	for i := 0; i < nbNodes; i++ {
		shareBytes := make([]byte, 4)
		for j := 0; j < 4; j++ {
			shareBytes[j] = byte(4*i + j)
		}
		fakeShares[i] = &reedsolomon.Encoding{
			Idx: int64(i),
			Val: shareBytes,
		}
	}
	hash := []byte{5, 6, 7, 8} // Arbitrary
	shareIdx := 0

	// Create an ECHO message
	echoMsg := createEchoMessage(fakeShares, hash)
	packet := &typedefs.Packet{
		Message: &typedefs.Packet_RbcMessage{
			RbcMessage: echoMsg,
		},
	}
	echoBytes, err := proto.Marshal(packet)
	require.NoError(t, err)

	// Sent t+1 ECHO and the node should do nothing (since no READY message has been sent, the
	// threshold for ECHO is 2t+1)
	echoThreshold := threshold + 1
	sent := 0
	for i := 0; i < echoThreshold; i++ {
		err := iface.Send(echoBytes, node.GetID())
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
		readyMsg := createReadyMessage(fakeShares[shareIdx].Val, hash, fakeShares[shareIdx].Idx)
		packet = &typedefs.Packet{
			Message: &typedefs.Packet_RbcMessage{
				RbcMessage: readyMsg,
			},
		}
		readyBytes, err := proto.Marshal(packet)
		require.NoError(t, err)
		err = iface.Send(readyBytes, node.GetID())
		require.NoError(t, err)
		sent++
		shareIdx++

		time.Sleep(10 * time.Millisecond)

		nReceived := node.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := node.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	// Send the t+1 and t+2 READY message and expect one READY broadcast
	readyMsg := createReadyMessage(fakeShares[shareIdx].Val, hash, fakeShares[shareIdx].Idx)
	packet = &typedefs.Packet{
		Message: &typedefs.Packet_RbcMessage{
			RbcMessage: readyMsg,
		},
	}
	readyBytes, err := proto.Marshal(packet)
	require.NoError(t, err)
	err = iface.Send(readyBytes, node.GetID())
	require.NoError(t, err)

	shareIdx++
	readyMsg = createReadyMessage(fakeShares[shareIdx].Val, hash, fakeShares[shareIdx].Idx)
	packet = &typedefs.Packet{
		Message: &typedefs.Packet_RbcMessage{
			RbcMessage: readyMsg,
		},
	}
	readyBytes, err = proto.Marshal(packet)
	require.NoError(t, err)
	err = iface.Send(readyBytes, node.GetID())
	require.NoError(t, err)
	sent += 2

	time.Sleep(10 * time.Millisecond)

	nReceived := node.GetReceived()
	// Should have received all messages plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent := node.GetSent()
	// Should have sent one READY message
	require.Equal(t, 1, len(nSent))
	packet = &typedefs.Packet{}
	err = proto.Unmarshal(nSent[0], packet)
	require.NoError(t, err)
	rbcMessage, ok := packet.GetMessage().(*typedefs.Packet_RbcMessage)
	require.True(t, ok, "Packet received should be a RBC message")
	_, ok = rbcMessage.RbcMessage.GetOp().(*typedefs.RBCMessage_ReadyInst)
	require.True(t, ok, "Message received should be a READY message")

	received := iface.GetReceived()
	// Should all have received a ready message
	require.Equal(t, 1, len(received))
	packet = &typedefs.Packet{}
	err = proto.Unmarshal(received[0], packet)
	require.NoError(t, err)
	rbcMessage, ok = packet.GetMessage().(*typedefs.Packet_RbcMessage)
	require.True(t, ok, "Packet received should be a RBC message")
	_, ok = rbcMessage.RbcMessage.GetOp().(*typedefs.RBCMessage_ReadyInst)
	require.True(t, ok, "Message received should be a READY")

	// Stop all
	cancel()
}

/***********************************************************************************************/
/************************************** Integration Tests **************************************/
/***********************************************************************************************/

// runBroadcast takes the node at index 0 from the given list of nodes and tells it to start RBC with the given
// message msg. All other nodes are set to listen. The method returns when the algorithm finished
// for all nodes
func runBroadcast(ctx context.Context, t require.TestingT, nodes []*TestNode, msg, hash []byte,
	onClose error, expectSuccess bool) {
	startNodes(ctx, t, nodes, onClose)

	wg := waitForResult(ctx, t, nodes, hash, expectSuccess)

	// Start RBC
	dealer := nodes[0]
	_, err := dealer.rbc.RBroadcast(msg)
	require.NoError(t, err)

	// Wait enough time for the nodes to have received the PROPOSE message
	time.Sleep(1 * time.Second)

	//require.True(t, instance.PredicatePassed())

	// Wait for all instances to finish
	wg.Wait()
}

// checkRBCResult checks that the state of the given node reflect a successful completion of
// an RBC algorithm given the reliably broadcast message msg. Doesn't return true of false but
// makes the testing fail in case of a problem
func checkRBCResult(t *testing.T, nodes []*TestNode, msg, msgHash []byte) {
	for _, node := range nodes {
		require.Equal(t, 1, len(node.rbc.GetInstances()))
		state, ok := node.rbc.GetState(msgHash)
		require.True(t, ok)
		require.True(t, state.Finished())
		val := state.GetValue()
		require.True(t, len(msg) == len(val))
		require.True(t, bytes.Equal(msg, val))
	}
}

func runAndCheckRBC(ctx context.Context, t *testing.T, nodes []*TestNode, msg, msgHash []byte) {
	runBroadcast(ctx, t, nodes, msg, msgHash, context.Canceled, true)
	checkRBCResult(t, nodes, msg, msgHash)
}

// TestFourRoundsRBC_Simple creates a network with a threshold t=2 and n=3*t+1 nodes
// and start a broadcast from one node. Wait until the algorithm finishes for all nodes
// and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_Simple(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewFakeNetwork()

	threshold := 2

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Run RBC and check the result
	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

// TestFourRoundsRBC_SimplePadding run the network with
// an unconventional message size and check that padding
// the message works
func TestFourRoundsRBC_SimplePadding(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewFakeNetwork()

	threshold := 2

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 2)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Run RBC and check the result
	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

func setupWithDeadNodes(t *testing.T, threshold, nbDead int) []*TestNode {
	// Config
	network := networking.NewFakeNetwork()
	nbNodes := 3*threshold + 1

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	nodesAlive := nodes[:nbNodes-nbDead]

	return nodesAlive
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// Wait until the algorithm finishes for all nodes and verifies that everyone agreed on the same value.
// In this situation, 1 node is dead, and we expect the algorithm to finish for all nodes alive correctly.
func TestFourRoundsRBC_OneDeadNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	threshold := 2
	nbDead := 1

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes := setupWithDeadNodes(t, threshold, nbDead)

	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// Wait until the algorithm finishes for all nodes and verifies that everyone agreed on the same value.
// In this situation, 2 nodes are dead, and we expect the algorithm to finish for all nodes alive correctly.
func TestFourRoundsRBC_TwoDeadNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	threshold := 2
	nbDead := 2

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes := setupWithDeadNodes(t, threshold, nbDead)

	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// In this situation, 3 nodes are dead, and we expect the algorithm to never finish.
func TestFourRoundsRBC_ThreeDeadNode(t *testing.T) {
	timeout := time.Second * 5
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	threshold := 2
	nbDead := 3

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes := setupWithDeadNodes(t, threshold, nbDead)

	// Run RBC in a go routing and call cancel on the context when the method returns (i.e. when the algorithm ends)
	runBroadcast(ctx, t, nodes, message, hash, context.DeadlineExceeded, false)

	// Wait for the context
	<-ctx.Done()

	// Require the deadline to have exceeded
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	cancel()
}

// TestFourRoundsRBC_SlowNode creates a network with a threshold t=2 and n=3*t+1
// nodes and start a broadcast from one node. In this situation, 1 node is slow
// i.e. it has some delay when reading and writing to the network. The broadcast
// should still work fine, only slowly.
func TestFourRoundsRBC_SlowNode(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewFakeNetwork()

	threshold := 2
	delay := 500 * time.Millisecond

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Add delay to the first node
	network.DelayNode(nodes[0].GetID(), delay)

	// Run and check RBC, it should work normally only slower
	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

// TestFourRoundsRBC_SlowNodes creates a network with a threshold t=2 and n=3*t+1
// nodes and start a broadcast from one node. In this situation, 3 node are slow
// i.e. they have some delay when reading and writing to the network. The broadcast
// should still work fine, only slowly.
func TestFourRoundsRBC_SlowNodes(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewFakeNetwork()

	threshold := 2
	nbSlow := 3
	delay := 500 * time.Millisecond
	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	message, hash := generateMessage(mLen)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Add delay to first three nodes
	for i := 0; i < nbSlow; i++ {
		network.DelayNode(nodes[i].GetID(), delay)
	}

	// Run and check RBC, it should work normally only slower
	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

// TestFourRoundsRBC_SlowNodesStress create a network where all nodes
// are slow in their communication. Expect the protocol to finish normally
// albeit slower
func TestFourRoundsRBC_SlowNodesStress(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewFakeNetwork()

	threshold := 10
	delay := time.Millisecond * 200

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Delay all nodes
	for _, node := range nodes {
		network.DelayNode(node.GetID(), delay)
	}

	// Run and check RBC, it should work normally only slower
	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

// TestFourRoundsRBC_DealAndDies creates a network with a threshold t=2 and n=3*t+1
// nodes and start a broadcast from one node. In this situation, the dealer deals
// and then dies immediately. We expect the algorithm to finish correctly for all other nodes.
func TestFourRoundsRBC_DealAndDies(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())

	threshold := 2

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Start all nodes expect the first who will deal and die
	startNodes(ctx, t, nodes[1:], context.Canceled)

	// Create a dying dealer. We don't need to actually create an RBC instance, we just need to send the PROPOSE
	// from some node in the network and never answer after that
	dyingDealer := nodes[0].NetworkInterface
	require.NoError(t, err)

	// Start RBC from the dying dealer
	proposeMessage := createProposeMessage(message)
	packet := &typedefs.Packet{
		Message: &typedefs.Packet_RbcMessage{
			RbcMessage: proposeMessage,
		},
	}
	out, err := proto.Marshal(packet)
	require.NoError(t, err)

	nodesAlive := nodes[1:]
	wg := waitForResult(ctx, t, nodesAlive, hash, true)

	// Broadcast the initial PROPOSE message to start RBC
	err = dyingDealer.Broadcast(out)
	require.NoError(t, err)
	t.Log("Broadcast complete")

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodesAlive, message, hash)
	cancel()
}

// TestFourRoundsRBC_CancelContext tests that cancelling the context stops the nodes
// after they have been started
func TestFourRoundsRBC_CancelContext(t *testing.T) {
	network := networking.NewFakeNetwork()
	threshold := 2

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Listen on the node in a go routine and expect it to cancel the context before the timeout
	ctx, cancel := context.WithCancel(context.Background())

	startNodes(ctx, t, nodes, context.Canceled)

	cancel()
}

func runWithDyingListeners(t *testing.T, threshold, nbDying int) {
	// Config
	network := networking.NewFakeNetwork()
	ctx, cancel := context.WithCancel(context.Background())
	delay := time.Millisecond * 100

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	message, hash := generateMessage(mLen)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Add a bit of delay to all nodes to make sure we have enough time to stop the node
	for _, node := range nodes {
		network.DelayNode(node.GetID(), delay)
	}

	dyingNodes := nodes[:nbDying]
	dealer := nodes[nbDying]
	nodesAlive := nodes[nbDying:]

	// Start all nodes expect the first who will have its own context to be cancelled
	startNodes(ctx, t, nodesAlive, context.Canceled)
	failingCtx, failingCancel := context.WithCancel(context.Background())
	startNodes(failingCtx, t, dyingNodes, context.Canceled)

	wg := waitForResult(ctx, t, nodesAlive, hash, true)
	wgDead := waitForResult(failingCtx, t, dyingNodes, hash, false)

	// Start RBC from the second node
	_, err = dealer.rbc.RBroadcast(message)
	require.NoError(t, err)

	// Stop the failing nodes
	time.Sleep(time.Millisecond * 30)
	failingCancel()

	wg.Wait()
	wgDead.Wait()

	// Check that everything worked for all nodes alive
	checkRBCResult(t, nodesAlive, message, hash)

	// Check that the last node did not finish
	for _, dyingNode := range dyingNodes {
		require.Nil(t, dyingNode.rbc.GetFinalValue(hash))
	}
	cancel()
}

// TestFourRoundsRBC_ListenerDies works similarly to TestFourRoundsRBC_DealAndDies but instead a random node is
// stopped and not the dealer
func TestFourRoundsRBC_ListenerDies(t *testing.T) {
	runWithDyingListeners(t, 2, 1)
}

// TestFourRoundsRBC_ListenerDies works similarly to TestFourRoundsRBC_DealAndStop but instead two random nodes are
// stopped and not the dealer
func TestFourRoundsRBC_TwoListenerDies(t *testing.T) {
	runWithDyingListeners(t, 2, 2)
}

// TestFourRoundsRBC_Stress creates a network with a threshold t=20 and n=3*t+1
// nodes and start a broadcast from one node. Wait until the algorithm finishes
// for all nodes and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_Stress(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewFakeNetwork()

	threshold := 20

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Run RBC and check the result
	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

// TestFourRoundsRBC_RealNetwork creates a UDP network with a threshold t=2 and n=3*t+1 nodes
// and start a broadcast from one node. Wait until the algorithm finishes for all nodes
// and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_RealNetwork(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewTransportNetwork(udp.NewUDP())
	threshold := 2

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Run RBC and check the result
	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

// TestFourRoundsRBC_RealNetworkStress creates a large UDP network with a threshold t=20 and n=3*t+1 nodes
// and start a broadcast from one node. Wait until the algorithm finishes for all nodes
// and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_RealNetworkStress(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewTransportNetwork(udp.NewUDP())
	threshold := 40

	// Randomly generate the value to broadcast
	message, hash := generateMessage(threshold + 1)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	// Run RBC and check the result
	runAndCheckRBC(ctx, t, nodes, message, hash)
	cancel()
}

func TestFourRoundsRBC_MultipleInstances(t *testing.T) {
	// Config
	ctx, cancel := context.WithCancel(context.Background())
	network := networking.NewTransportNetwork(udp.NewUDP())
	threshold := 2
	nbNodes := threshold*3 + 1

	nbInstances := 3
	require.LessOrEqual(t, nbInstances, nbNodes, "Need at least %d nodes to run %d instances", nbInstances, nbInstances)

	nodes, err := createNetwork(network, threshold)
	require.NoError(t, err)

	startNodes(ctx, t, nodes, context.Canceled)

	wgs := sync.WaitGroup{}
	for i := 0; i < nbInstances; i++ {
		message, hash := generateMessage(threshold + 1)
		dealer := nodes[i]

		wg := waitForResult(ctx, t, nodes, hash, true)

		_, err := dealer.rbc.RBroadcast(message)
		require.NoError(t, err)

		wgs.Add(1)
		go func() {
			wg.Wait()
			checkRBCResult(t, nodes, message, hash)
			wgs.Done()
		}()
	}

	wgs.Wait()

	cancel()
}
