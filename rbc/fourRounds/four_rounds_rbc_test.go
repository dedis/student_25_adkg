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
	Network    networking.NetworkInterface[[]byte]
	rcvChan    <-chan []byte
	readDelay  time.Duration
	writeDelay time.Duration
}

func NewMockAuthStream(iface networking.NetworkInterface[[]byte], readDelay, writeDelay time.Duration) *MockAuthStream {
	return &MockAuthStream{
		Network:    iface,
		rcvChan:    make(chan []byte),
		readDelay:  readDelay,
		writeDelay: writeDelay,
	}
}

func NoDelayMockAuthStream(iface networking.NetworkInterface[[]byte]) *MockAuthStream {
	return &MockAuthStream{
		Network:    iface,
		rcvChan:    make(chan []byte),
		readDelay:  0,
		writeDelay: 0,
	}
}

func (iface *MockAuthStream) Broadcast(bytes []byte) error {
	// Artificially delay the broadcast
	time.Sleep(iface.writeDelay)
	return iface.Network.Broadcast(bytes)
}

func (iface *MockAuthStream) Receive(stop <-chan struct{}) ([]byte, error) {
	msg, err := iface.Network.Receive(stop)
	time.Sleep(iface.readDelay) // Artificially delay receiving
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
			_, _ = stream.Receive(ctx.Done())
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
	stream := NoDelayMockAuthStream(nIface)
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
		cancellers[i] = startDummyNode(NoDelayMockAuthStream(iface))
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
	t.Logf("Sent PROPOSE message to %d", nIface.GetID())

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

	// Stop all listening networks
	for _, canceller := range cancellers {
		canceller()
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
	stream := NoDelayMockAuthStream(nIface)
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
		cancellers[i] = startDummyNode(NoDelayMockAuthStream(iface))
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
	stream := NoDelayMockAuthStream(nIface)
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
		cancellers[i] = startDummyNode(NoDelayMockAuthStream(iface))
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
	stream := NoDelayMockAuthStream(nIface)
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
		cancellers[i] = startDummyNode(NoDelayMockAuthStream(iface))
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

// checkRBCResult checks that the state of the given node reflect a successful completion of
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

// TestFourRoundsRBC_Simple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
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
		stream := NoDelayMockAuthStream(network.JoinNetwork())
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
		stream := NoDelayMockAuthStream(network.JoinNetwork())
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
		stream := NoDelayMockAuthStream(network.JoinNetwork())
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
		stream := NoDelayMockAuthStream(network.JoinNetwork())
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
		stream := NoDelayMockAuthStream(network.JoinNetwork())
		deadNodes[i] = stream
	}

	// Run and check RBC
	runAndCheckRBC(t, nodes, nbNodes-nbDead, s)
}

// TestFourRoundsRBCSimple creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
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
		stream := NoDelayMockAuthStream(network.JoinNetwork())
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
		stream := NoDelayMockAuthStream(network.JoinNetwork())
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

// TestFourRoundsRBC_SlowNode creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// In this situation, 1 node is slow i.e. it has some delay when reading and writing to the network. The broadcast
// should still work fine, only slowly.
func TestFourRoundsRBC_SlowNode(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbSlow := 1
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		var stream *MockAuthStream
		if i >= nbNodes-nbSlow {
			// Set up the slow nodes with half a second of delay when reading or writing
			stream = NewMockAuthStream(network.JoinNetwork(), time.Millisecond*500, time.Millisecond*500)
		} else {
			// All other nodes don't have delay
			stream = NoDelayMockAuthStream(network.JoinNetwork())
		}
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Run and check RBC, it should work normally only slower
	runAndCheckRBC(t, nodes, nbNodes, s)
}

// TestFourRoundsRBC_SlowNode creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// In this situation, 3 node are slow i.e. they have some delay when reading and writing to the network. The broadcast
// should still work fine, only slowly.
func TestFourRoundsRBC_SlowNodes(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbSlow := 3
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		var stream *MockAuthStream
		if i > nbNodes-nbSlow {
			// Set up the slow nodes with half a second of delay when reading or writing
			stream = NewMockAuthStream(network.JoinNetwork(), time.Millisecond*500, time.Millisecond*500)
		} else {
			// All other nodes don't have delay
			stream = NoDelayMockAuthStream(network.JoinNetwork())
		}
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Run and check RBC, it should work normally only slower
	runAndCheckRBC(t, nodes, nbNodes, s)
}

// TestFourRoundsRBC_DealAndDies creates a network with a threshold t=2 and n=3*t+1 nodes and start a broadcast from one node.
// In this situation, the dealer deals and then dies immediately. We expect the algorithm to finish correctly for all other nodes.
func TestFourRoundsRBC_DealAndDies(t *testing.T) {
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
		stream := NoDelayMockAuthStream(network.JoinNetwork())
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Set all nodes to listen except from one which will be the dead one
	// Create a wait group to wait for all instances to finish
	wg := sync.WaitGroup{}
	for i := 0; i < nbNodes-1; i++ {
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

	// Create a dying dealer. We don't need to actually create an RBC instance, we just need to send the PROPOSE
	// from some node in the network and never answer after that
	dyingDealer := NoDelayMockAuthStream(network.JoinNetwork())

	// Start RBC from the dying dealer

	// Marshall the message
	marshaller := getMarshaller(g)
	msBytes := make([][]byte, len(s))
	for i, m := range s {
		b, err := marshaller.Marshal(m)
		require.NoError(t, err)
		msBytes[i] = b
	}
	inst := createProposeMessage(msBytes)
	out, err := proto.Marshal(inst)
	require.NoError(t, err)

	// Broadcast the initial PROPOSE message to start RBC
	err = dyingDealer.Broadcast(out)
	require.NoError(t, err)
	t.Log("Broadcast complete")

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodes, nbNodes-1, s)
}

// TestFourRoundsRBC_testStop tests that calling the Stop method on a node after having called the broadcast
// or listen function returns without error
func TestFourRoundsRBC_testStop(t *testing.T) {
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	g := edwards25519.NewBlakeSHA256Ed25519()

	stream := NoDelayMockAuthStream(network.JoinNetwork())
	marshaller := &ScalarMarshaller{
		Group: g,
	}
	node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
		r, nbNodes, uint32(0)))

	// Listen on the node in a go routine and expect it to cancel the context before the timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)

	go func() {
		err := node.rbc.Listen()
		// Listen should have returned an ErrClosed since the stop function should have been called
		require.NoError(t, err)
		// If this is reached, the Listen method is returned so cancel the ctx to notify
		cancel()
	}()

	err := node.rbc.Stop()
	require.NoError(t, err)

	<-ctx.Done()
	require.Equal(t, context.Canceled, ctx.Err())

	// Similarly but with broadcast

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	ctx, cancel = context.WithTimeout(context.Background(), time.Second*1)

	go func() {
		err := node.rbc.RBroadcast(s)
		// Listen should have returned an ErrClosed since the stop function should have been called
		require.NoError(t, err)
		// If this is reached, the Listen method is returned so cancel the ctx to notify
		cancel()
	}()

	err = node.rbc.Stop()
	require.NoError(t, err)

	<-ctx.Done()
	require.Equal(t, context.Canceled, ctx.Err())
}

// TestFourRoundsRBC_DealAndStop works similarly to TestFourRoundsRBC_DealAndDies but instead the Stop function
// is called on the dealer
func TestFourRoundsRBC_DealAndStop(t *testing.T) {
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
		// Add a bit of delay when reading to leave time for the dealer node to be stopped
		stream := NewMockAuthStream(network.JoinNetwork(), time.Millisecond*10, 0)
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Set all nodes to listen except from one which will be the dealer
	wg := sync.WaitGroup{}
	for i := 0; i < nbNodes-1; i++ {
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

	dealer := nodes[nbNodes-1]

	go func() {
		// Start RBC from the dealer
		err := dealer.rbc.RBroadcast(s)
		require.NoError(t, err)
		t.Log("Broadcast complete")

	}()

	// Stop the dealer
	time.Sleep(time.Millisecond * 10)
	err := dealer.rbc.Stop()
	require.NoError(t, err)
	t.Log("Dealer Stopped")

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodes, nbNodes-1, s)

	// Check the dealer did not finish
	require.False(t, dealer.rbc.finished)
}

// TestFourRoundsRBC_ListenerDies works similarly to TestFourRoundsRBC_DealAndStop but instead a random node is
// stopped and not the dealer
func TestFourRoundsRBC_ListenerDies(t *testing.T) {
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
		// Add a bit of delay when reading to leave time for the dealer node to be stopped
		stream := NewMockAuthStream(network.JoinNetwork(), time.Millisecond*10, 0)
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Set all nodes to listen except from one which will be the dealer and the one that will fail
	wg := sync.WaitGroup{}
	for i := 1; i < nbNodes-1; i++ {
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

	// Start the failing node
	wg.Add(1)
	go func() {
		defer wg.Done()
		failNode := nodes[nbNodes-1]
		err := failNode.rbc.Listen()
		require.NoError(t, err)
		t.Log("Failing node finished")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Start RBC from the dealer
		err := nodes[0].rbc.RBroadcast(s)
		require.NoError(t, err)
		t.Log("Broadcast complete")
	}()

	// Wait  little bit
	time.Sleep(time.Millisecond * 30)
	// Stop the last node
	err := nodes[nbNodes-1].rbc.Stop()
	require.NoError(t, err)

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodes, nbNodes-1, s)

	// Check that the last node did not finish
	require.False(t, nodes[nbNodes-1].rbc.finished)
}

// TestFourRoundsRBC_ListenerDies works similarly to TestFourRoundsRBC_DealAndStop but instead two random nodes are
// stopped and not the dealer
func TestFourRoundsRBC_TwoListenerDies(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 2
	r := 2
	nbNodes := 3*threshold + 1
	nbFailing := 2
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		// Add a bit of delay when reading to leave time for the dealer node to be stopped
		stream := NewMockAuthStream(network.JoinNetwork(), time.Millisecond*10, 0)
		marshaller := &ScalarMarshaller{
			Group: g,
		}
		node := NewTestNode(stream, NewFourRoundRBC[kyber.Scalar](pred, sha256.New(), threshold, stream, marshaller, g,
			r, nbNodes, uint32(i)))
		nodes[i] = node
	}

	// Set all nodes to listen except from one which will be the dealer and the one that will fail
	wg := sync.WaitGroup{}
	for i := 1; i < nbNodes-nbFailing; i++ {
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

	// Start the failing nodes
	for i := 0; i < nbFailing; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failNode := nodes[nbNodes-i-1]
			err := failNode.rbc.Listen()
			require.NoError(t, err)
			t.Logf("Failing node %d finished", i)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Start RBC from the dealer
		err := nodes[0].rbc.RBroadcast(s)
		require.NoError(t, err)
		t.Log("Broadcast complete")
	}()

	// Wait  little bit
	// Stop the failing nodes
	time.Sleep(time.Millisecond * 20)
	for i := 0; i < nbFailing; i++ {
		time.Sleep(time.Millisecond * 10)
		err := nodes[nbNodes-i-1].rbc.Stop()
		require.NoError(t, err)
	}

	wg.Wait()

	// Check that everything worked
	checkRBCResult(t, nodes, nbNodes-nbFailing, s)

	// Check that the last node did not finish
	for i := 0; i < nbFailing; i++ {
		require.False(t, nodes[nbNodes-i-1].rbc.finished)
	}
}

// TestFourRoundsRBC_Stress creates a network with a threshold t=20 and n=3*t+1 nodes and start a broadcast from one node.
// Wait until the algorithm finishes for all nodes and verifies that everyone agreed on the same value.
func TestFourRoundsRBC_Stress(t *testing.T) {
	// Config
	network := networking.NewFakeNetwork[[]byte]()

	threshold := 20
	r := 2
	nbNodes := 3*threshold + 1
	g := edwards25519.NewBlakeSHA256Ed25519()

	// Randomly generate the value to broadcast
	mLen := threshold + 1 // Arbitrary message length
	s := generateMessage(mLen, g, g.RandomStream())

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		stream := NoDelayMockAuthStream(network.JoinWithBuffer(4000))
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
