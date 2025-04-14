package fourRounds

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"go.dedis.ch/kyber/v4/share"
	"google.golang.org/protobuf/proto"
	"student_25_adkg/networking"
	"student_25_adkg/rbc/fourRounds/typedefs"
	"sync"
	"testing"
	"time"
)

type TestNode struct {
	g     kyber.Group
	iface *MockAuthStream
	rbc   *FourRoundRBC[kyber.Scalar]
	stop  bool
}

// MockAuthStream mocks an authenticated message stream. Nothing is actually authenticated.
type MockAuthStream struct {
	Network networking.NetworkInterface[[]byte]
}

func NewMockAuthStream(iface networking.NetworkInterface[[]byte]) *MockAuthStream {
	return &MockAuthStream{
		Network: iface,
	}
}

func (iface *MockAuthStream) Broadcast(bytes []byte) error {
	return iface.Network.Broadcast(bytes)
}

func (iface *MockAuthStream) Receive() ([]byte, error) {
	msg, err := iface.Network.Receive()
	return msg, err
}

func NewTestNode(iface *MockAuthStream, rbc *FourRoundRBC[kyber.Scalar]) *TestNode {
	return &TestNode{
		iface: iface,
		rbc:   rbc,
	}
}

func startDummyNode(stream *MockAuthStream) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			_, _ = stream.Receive()
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return cancel
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

func TestFreshHash(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate a message to hash
	mLen := 2 // Arbitrary message length
	s := make([]kyber.Scalar, mLen)
	for i := 0; i < mLen; i++ {
		s[i] = g.Scalar().Pick(g.RandomStream())
	}

	marshaller := &ScalarMarshaller{
		Group: g,
	}
	node := NewTestNode(nil, NewFourRoundRBC(pred, sha256.New(), 0, nil, marshaller, g, 0, 0, 0))

	h1, err := node.rbc.FreshHash(s)
	require.NoError(t, err)

	h2, err := node.rbc.FreshHash(s)
	require.NoError(t, err)

	require.Equal(t, h1, h2)
}

func getMarshaller(g kyber.Group) *ScalarMarshaller {
	return &ScalarMarshaller{
		Group: g,
	}
}

func generateMessage(l int, g kyber.Group, randomStream cipher.Stream) []kyber.Scalar {
	s := make([]kyber.Scalar, l)
	for i := 0; i < l; i++ {
		s[i] = g.Scalar().Pick(randomStream)
	}
	return s
}

func marshallMessage(msg []kyber.Scalar, group kyber.Group) [][]byte {
	marshaller := getMarshaller(group)
	ms := make([][]byte, len(msg))
	for i := 0; i < len(msg); i++ {
		m, err := marshaller.Marshal(msg[i])
		if err != nil {
			panic(err)
		}
		ms[i] = m
	}
	return ms
}

// TestFourRoundsRBC_Receive_Propose tests that a node correctly handles the reception of a PROPOSE message
func TestFourRoundsRBC_Receive_Propose(t *testing.T) {

	// Set up a fake network
	network := networking.NewFakeNetwork[[]byte]()

	// Config
	g := edwards25519.NewBlakeSHA256Ed25519()
	r := 2 // Reconstruction
	threshold := 2
	nbNodes := 3*threshold + 1
	mLen := threshold + 1 // Length of the messages being sent

	// Set up a node to test
	nIface := network.JoinNetwork()
	stream := NewMockAuthStream(nIface)
	marshaller := getMarshaller(g)
	node := NewTestNode(stream, NewFourRoundRBC(pred, sha256.New(), threshold, stream, marshaller, g, r, nbNodes, nIface.GetID()))
	go func() {
		err := node.rbc.Listen()
		require.NoError(t, err)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	interfaces := make([]*networking.FakeInterface[[]byte], nbNodes-1)
	cancellers := make([]context.CancelFunc, nbNodes-1)
	for i := 0; i < nbNodes-1; i++ {
		iface := network.JoinNetwork()
		interfaces[i] = iface
		cancellers[i] = startDummyNode(NewMockAuthStream(iface))
	}

	// Send a PROPOSE message to the test node and check that it answers correctly
	s := generateMessage(mLen, g, g.RandomStream())
	sHash, err := node.rbc.FreshHash(s)
	require.NoError(t, err)
	proposeMessage := createProposeMessage(marshallMessage(s, g))
	proposeBytes, err := proto.Marshal(proposeMessage)
	if err != nil {
		panic(err)
	}

	err = interfaces[1].Send(proposeBytes, nIface.GetID())
	require.NoError(t, err)
	t.Logf("Send PROPOSE message to %d", nIface.GetID())

	// Wait a second for message to have been sent
	time.Sleep(10 * time.Millisecond)

	// Stop all listening networks
	for _, canceller := range cancellers {
		canceller()
	}

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
				require.Equal(t, uint32(j), op.EchoInst.I)
				require.True(t, bytes.Equal(op.EchoInst.H, sHash))
			default:
				require.Fail(t, "Unexpected message type: %T", msg)
			}
		}
	}

	// Check the messages sent and received by the real node
	nSent := nIface.GetSent()

	// Node should have sent a broadcast to each node
	require.Equal(t, nbNodes, len(nSent))

	nReceived := nIface.GetReceived()
	// Node should have received the PROPOSE message and its own broadcasts
	require.Equal(t, 1+nbNodes, len(nReceived))

	// Try to reconstruct from the encoded messages
	messages := interfaces[0].GetReceived()
	chunks := make([]*share.PriShare, nbNodes)
	for i := 0; i < len(messages); i++ {
		bs := messages[i]
		msg := &typedefs.Instruction{}
		err = proto.Unmarshal(bs, msg)
		require.NoError(t, err)
		switch op := msg.Operation.Op.(type) {
		case *typedefs.Message_EchoInst:
			chunk, err := marshaller.Unmarshal(op.EchoInst.Mi)
			require.NoError(t, err)
			chunks[op.EchoInst.I] = &share.PriShare{
				I: op.EchoInst.I,
				V: chunk,
			}
		default:
			require.Fail(t, "Unexpected message type: %T", msg)
		}
	}

	require.Equal(t, nbNodes, len(chunks))
	decoded, err := node.rbc.rs.Decode(chunks, threshold+1)
	require.NoError(t, err)

	// Check that the bytes of the decoded message match the original message sent
	for i := 0; i < threshold+1; i++ {
		require.Equal(t, s[i], decoded[i])
	}
}

// TestFourRoundsRBC_Receive_Echo checks that a node correctly handles receiving ECHO messages
// Send 2t+1 ECHO messages checking that nothing happens before the 2t+1-th is received and
// then a READY is sent only once
func TestFourRoundsRBC_Receive_Echo(t *testing.T) {
	// Set up a fake network
	network := networking.NewFakeNetwork[[]byte]()

	// Config
	g := edwards25519.NewBlakeSHA256Ed25519()
	r := 2 // Reconstruction
	threshold := 2
	nbNodes := 3*threshold + 1

	// Set up a node to test
	nIface := network.JoinNetwork()
	stream := NewMockAuthStream(nIface)
	marshaller := getMarshaller(g)
	node := NewTestNode(stream, NewFourRoundRBC(pred, sha256.New(), threshold, stream, marshaller, g, r, nbNodes, nIface.GetID()))
	go func() {
		err := node.rbc.Listen()
		require.NoError(t, err)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	intefaces := make([]*networking.FakeInterface[[]byte], nbNodes-1)
	cancellers := make([]context.CancelFunc, nbNodes-1)
	for i := 0; i < nbNodes-1; i++ {
		iface := network.JoinNetwork()
		intefaces[i] = iface
		// Start the interface to just receive messages and do nothing with them
		cancellers[i] = startDummyNode(NewMockAuthStream(iface))
	}

	fakeMi := []byte{1, 2, 3, 4} // Arbitrary
	hash := []byte{5, 6, 7, 8}
	echoMsg := createEchoMessage(fakeMi, hash, nIface.GetID())
	echoBytes, err := proto.Marshal(echoMsg)
	require.NoError(t, err)

	echoThreshold := 2*threshold + 1
	sent := 0
	for i := 0; i < echoThreshold-1; i++ {
		// Send the ECHO message to the real node
		err := intefaces[0].Send(echoBytes, nIface.GetID())
		require.NoError(t, err)
		sent += 1
		// Wait a few milliseconds to make sure the node received and processed the message
		time.Sleep(10 * time.Millisecond)

		nReceived := nIface.GetReceived()
		// Should have received every message
		require.Equal(t, sent, len(nReceived))
		nSent := nIface.GetSent()
		// Should not have sent anything yet
		require.Equal(t, 0, len(nSent))
	}

	// Sent an ECHO message for another share of the encoding and expect nothing to happen
	echoMsg2 := createEchoMessage(fakeMi, hash, intefaces[0].GetID())
	echoBytes2, err := proto.Marshal(echoMsg2)
	require.NoError(t, err)
	err = intefaces[0].Send(echoBytes2, nIface.GetID())
	require.NoError(t, err)
	sent += 1

	time.Sleep(10 * time.Millisecond)

	nReceived := nIface.GetReceived()
	// Should have received all messages
	require.Equal(t, sent, len(nReceived))
	nSent := nIface.GetSent()
	// Should have sent nothing yet
	require.Equal(t, 0, len(nSent))

	// Sent another echo and expect a ready message to be broadcast
	err = intefaces[0].Send(echoBytes, nIface.GetID())
	require.NoError(t, err)
	sent += 1
	// Wait sometime to leave time for the node to have sent all its messages
	time.Sleep(10 * time.Millisecond)

	nReceived = nIface.GetReceived()
	// Should have received every ECHO message plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent = nIface.GetSent()
	// Should have sent a READY broadcast
	require.Equal(t, 1, len(nSent))

	// All other interfaces should have received one
	for _, iface := range intefaces {
		received := iface.GetReceived()
		require.Equal(t, 1, len(received))
	}

	// Send another ECHO and check that no other message is sent by the node
	err = intefaces[0].Send(echoBytes, nIface.GetID())
	require.NoError(t, err)
	sent += 1

	time.Sleep(10 * time.Millisecond)
	nReceived = nIface.GetReceived()
	// Should have received all messages plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent = nIface.GetSent()
	// Should only have sent a single ECHO message
	require.Equal(t, 1, len(nSent))
	for _, iface := range intefaces {
		received := iface.GetReceived()
		// Should only have received one ECHO from the node
		require.Equal(t, 1, len(received))
	}

	// Stop all
	for _, canceller := range cancellers {
		canceller()
	}
}

// TestFourRoundsRBC_Receive_Ready_before checks that a node correctly handles receiving READY messages
// Send t ready messages and expect nothing to be sent back. Then send t+1-th message
// and expect nothing until t+1 ECHO message for its corresponding share of the
// encoding is received
func TestFourRoundsRBC_Receive_Ready_before(t *testing.T) {
	// Set up a fake network
	network := networking.NewFakeNetwork[[]byte]()

	// Config
	g := edwards25519.NewBlakeSHA256Ed25519()
	r := 2 // Reconstruction
	threshold := 2
	nbNodes := 3*threshold + 1

	// Set up a node to test
	nIface := network.JoinNetwork()
	stream := NewMockAuthStream(nIface)
	marshaller := getMarshaller(g)
	node := NewTestNode(stream, NewFourRoundRBC(pred, sha256.New(), threshold, stream, marshaller, g, r, nbNodes, nIface.GetID()))
	go func() {
		err := node.rbc.Listen()
		require.NoError(t, err)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	interfaces := make([]*networking.FakeInterface[[]byte], nbNodes-1)
	cancellers := make([]context.CancelFunc, nbNodes-1)
	for i := 0; i < nbNodes-1; i++ {
		iface := network.JoinNetwork()
		interfaces[i] = iface
		// Start the interface to just receive messages and do nothing with them
		cancellers[i] = startDummyNode(NewMockAuthStream(iface))
	}

	fakeMi := []byte{1, 2, 3, 4} // Arbitrary
	hash := []byte{5, 6, 7, 8}
	readyMsg := createReadyMessage(fakeMi, hash, nIface.GetID())
	readyBytes, err := proto.Marshal(readyMsg)
	require.NoError(t, err)

	readyThreshold := threshold + 1

	// Send t READY messages and expect nothing each time
	sent := 0
	for i := 0; i < readyThreshold-1; i++ {
		err := interfaces[0].Send(readyBytes, nIface.GetID())
		require.NoError(t, err)
		sent += 1

		time.Sleep(10 * time.Millisecond)

		nReceived := nIface.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := nIface.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	// Send t+1 message and expect nothing
	err = interfaces[0].Send(readyBytes, nIface.GetID())
	require.NoError(t, err)
	sent += 1

	time.Sleep(10 * time.Millisecond)

	nReceived := nIface.GetReceived()
	require.Equal(t, sent, len(nReceived))

	nSent := nIface.GetSent()
	require.Equal(t, 0, len(nSent))

	for _, iface := range interfaces {
		received := iface.GetReceived()
		// Should not have received anything yet
		require.Equal(t, 0, len(received))
	}

	// Send t ECHO messages for the node and nothing should happen
	echoMsg := createEchoMessage(fakeMi, hash, nIface.GetID())
	echoBytes, err := proto.Marshal(echoMsg)
	require.NoError(t, err)

	echoThreshold := threshold + 1
	for i := 0; i < echoThreshold-1; i++ {
		err := interfaces[0].Send(echoBytes, nIface.GetID())
		require.NoError(t, err)
		sent += 1

		time.Sleep(10 * time.Millisecond)

		nReceived := nIface.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := nIface.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	// Sent t+1 and t+2 ECHO and the node should have sent one READY message
	err = interfaces[0].Send(echoBytes, nIface.GetID())
	require.NoError(t, err)
	err = interfaces[0].Send(echoBytes, nIface.GetID())
	require.NoError(t, err)
	sent += 2

	time.Sleep(10 * time.Millisecond)

	nReceived = nIface.GetReceived()
	// Should have received all messages plus its own READY broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent = nIface.GetSent()
	// Should have sent a READY broadcast
	require.Equal(t, 1, len(nSent))

	for _, iface := range interfaces {
		received := iface.GetReceived()
		// Should have received a READY broadcast from the node
		require.Equal(t, 1, len(received))
	}

}

// TestFourRoundsRBC_Receive_Ready_after does a  similar test to TestRBC_Receive_Ready_before but
// here the node receives the ECHO messages before receiving the t+1 ready message
func TestFourRoundsRBC_Receive_Ready_after(t *testing.T) {
	// Set up a fake network
	network := networking.NewFakeNetwork[[]byte]()

	// Config
	g := edwards25519.NewBlakeSHA256Ed25519()
	r := 2 // Reconstruction
	threshold := 2
	nbNodes := 3*threshold + 1

	// Set up a node to test
	nIface := network.JoinNetwork()
	stream := NewMockAuthStream(nIface)
	marshaller := getMarshaller(g)
	node := NewTestNode(stream, NewFourRoundRBC(pred, sha256.New(), threshold, stream, marshaller, g, r, nbNodes, nIface.GetID()))
	go func() {
		err := node.rbc.Listen()
		require.NoError(t, err)
	}()

	// Connect "fake" nodes i.e. get their interface without creating a test node since we just
	// want to see what these interfaces receive from the real node
	interfaces := make([]*networking.FakeInterface[[]byte], nbNodes-1)
	cancellers := make([]context.CancelFunc, nbNodes-1)
	for i := 0; i < nbNodes-1; i++ {
		iface := network.JoinNetwork()
		interfaces[i] = iface
		// Start the interface to just receive messages and do nothing with them
		cancellers[i] = startDummyNode(NewMockAuthStream(iface))
	}

	// Create a READY message
	fakeMi := []byte{1, 2, 3, 4} // Arbitrary
	hash := []byte{5, 6, 7, 8}   // Arbitrary
	readyMsg := createReadyMessage(fakeMi, hash, nIface.GetID())
	readyBytes, err := proto.Marshal(readyMsg)
	require.NoError(t, err)

	// Create an ECHO message
	echoMsg := createEchoMessage(fakeMi, hash, nIface.GetID())
	echoBytes, err := proto.Marshal(echoMsg)
	require.NoError(t, err)

	// Sent t+1 ECHO and the node should do nothing (since no READY message has been sent, the
	// threshold for ECHO is 2t+1)
	echoThreshold := threshold + 1
	sent := 0
	for i := 0; i < echoThreshold; i++ {
		err := interfaces[0].Send(echoBytes, nIface.GetID())
		require.NoError(t, err)
		sent += 1

		time.Sleep(10 * time.Millisecond)

		nReceived := nIface.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := nIface.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	readyThreshold := threshold + 1
	// Send t READY messages and expect nothing each time
	for i := 0; i < readyThreshold-1; i++ {
		err := interfaces[0].Send(readyBytes, nIface.GetID())
		require.NoError(t, err)
		sent += 1

		time.Sleep(10 * time.Millisecond)

		nReceived := nIface.GetReceived()
		// Should have received all messages
		require.Equal(t, sent, len(nReceived))

		nSent := nIface.GetSent()
		// Should not have sent anything
		require.Equal(t, 0, len(nSent))
	}

	// Send the t+1 and t+2 READY message and expect one READY broadcast
	err = interfaces[0].Send(readyBytes, nIface.GetID())
	require.NoError(t, err)
	err = interfaces[0].Send(readyBytes, nIface.GetID())
	require.NoError(t, err)
	sent += 2

	time.Sleep(10 * time.Millisecond)

	nReceived := nIface.GetReceived()
	// Should have received all messages plus its own broadcast
	require.Equal(t, sent+1, len(nReceived))

	nSent := nIface.GetSent()
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
}

/***********************************************************************************************/
/************************************** Integration Tests **************************************/
/***********************************************************************************************/

// runBroadcast takes the node at index 0 from the given list of nodes and tells it to start RBC with the given
// message msg. All other nodes are set to listen. The method returns when the algorithm finished
// for all nodes
func runBroadcast(t *testing.T, nodes []*TestNode, nbNodes int, msg []kyber.Scalar) {
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
	err := n1.rbc.RBroadcast(msg)
	t.Log("Broadcast complete")
	require.NoError(t, err)

	wg.Wait()
}

// checkRBCResult checks that the state of the given node reflect a successfully completion of
// an RBC algorithm given the reliably broadcast message msg. Doesn't return true of false but
// makes the testing fail in case of a problem
func checkRBCResult(t *testing.T, nodes []*TestNode, nbNodes int, msg []kyber.Scalar) {
	for i := 0; i < nbNodes; i++ {
		n := nodes[i]
		val := n.rbc.finalValue
		finished := n.rbc.finished
		require.True(t, finished)
		require.True(t, len(msg) == len(val))
		for i := 0; i < len(val); i++ {
			require.True(t, msg[i].Equal(val[i]))
		}
	}
}

func runAndCheckRBC(t *testing.T, nodes []*TestNode, nbNodes int, msg []kyber.Scalar) {
	runBroadcast(t, nodes, nbNodes, msg)
	checkRBCResult(t, nodes, nbNodes, msg)
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// Wait until the algorithm finishes for all nodes and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_Simple(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		stream := NewMockAuthStream(network.JoinNetwork())
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Run RBC and check the result
	runAndCheckRBC(t, nodes, nbNodes, s)
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// Wait until the algorithm finishes for all nodes and verifies that everyone agreed on the same value.
// In this situation, 1 node is dead and we expect the algorithm to finish for all nodes alive correctly.
func TestFourRoundsRBC_OneDeadNode(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbDead := 1
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	// Set up the working nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes-nbDead; i++ {
		stream := NewMockAuthStream(network.JoinNetwork())
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Set up the dead nodes
	deadNodes := make([]*MockAuthStream, nbDead)
	for i := 0; i < nbDead; i++ {
		// Dead nodes just mean a node that joins the network but never receives or sends anything
		// i.e. creating a stream but never using it
		stream := NewMockAuthStream(network.JoinNetwork())
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
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbDead := 2
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	// Set up the working nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes-nbDead; i++ {
		stream := NewMockAuthStream(network.JoinNetwork())
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Set up the dead nodes
	deadNodes := make([]*MockAuthStream, nbDead)
	for i := 0; i < nbDead; i++ {
		// Dead nodes just mean a node that joins the network but never receives or sends anything
		// i.e. creating a stream but never using it
		stream := NewMockAuthStream(network.JoinNetwork())
		deadNodes[i] = stream
	}

	// Run and check RBC
	runAndCheckRBC(t, nodes, nbNodes-nbDead, s)
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// Wait until the algorithm finishes for all nodes and verifies that everyone agreed on the same value.
// In this situation, 3 nodes are dead, and we expect the algorithm to never finish.
func TestFourRoundsRBC_ThreeDeadNode(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()

	timeout := time.Second * 5 // Wait five seconds
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbDead := 3
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	// Set up the working nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes-nbDead; i++ {
		stream := NewMockAuthStream(network.JoinNetwork())
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Set up the dead nodes
	deadNodes := make([]*MockAuthStream, nbDead)
	for i := 0; i < nbDead; i++ {
		// Dead nodes just mean a node that joins the network but never receives or sends anything
		// i.e. creating a stream but never using it
		stream := NewMockAuthStream(network.JoinNetwork())
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
