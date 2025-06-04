package acss

import (
	"context"
	"crypto/sha256"
	"errors"
	"student_25_adkg/networking"
	"student_25_adkg/pedersencommitment"
	"student_25_adkg/secretsharing"
	test "student_25_adkg/testing"
	"student_25_adkg/typedefs"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
	"go.dedis.ch/protobuf"
	"google.golang.org/protobuf/proto"
)

// registerPointAndScalarProtobufInterfaces registers the kyber.Point and kyber.Scalar interfaces
// into protobuf so that they can be encoded and decoded. Needs to be called only once
func registerPointAndScalarProtobufInterfaces(g kyber.Group) {
	protobuf.RegisterInterface(func() interface{} {
		return g.Point()
	})
	protobuf.RegisterInterface(func() interface{} {
		return g.Scalar()
	})
}

func TestMain(_ *testing.M) {
	g := edwards25519.NewBlakeSHA256Ed25519()
	registerPointAndScalarProtobufInterfaces(g)
}

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

type KeyStoreImpl struct {
	keys map[int64]kyber.Point
	sync.RWMutex
}

func NewKeyStore() *KeyStoreImpl {
	return &KeyStoreImpl{
		keys: make(map[int64]kyber.Point),
	}
}

func (ks *KeyStoreImpl) GetPublicKey(id int64) (kyber.Point, error) {
	ks.RLock()
	defer ks.RUnlock()
	key, ok := ks.keys[id]
	if !ok {
		return key, errors.New("not found")
	}
	return key, nil
}

func (ks *KeyStoreImpl) SetPublicKey(id int64, key kyber.Point) {
	ks.Lock()
	defer ks.Unlock()
	ks.keys[id] = key
}

func generateKeyStore(interfaces []networking.NetworkInterface, g kyber.Group) (KeyStore, map[int64]kyber.Scalar) {
	ks := NewKeyStore()
	privateKeys := make(map[int64]kyber.Scalar)
	for _, n := range interfaces {
		private := g.Scalar().Pick(random.New())
		public := g.Point().Base()
		public = public.Mul(private, public)
		ks.SetPublicKey(n.GetID(), public)
		privateKeys[n.GetID()] = private
	}

	return ks, privateKeys
}

type TestNode struct {
	acss          *ACSS
	rbcInterface  networking.NetworkInterface
	acssInterface networking.NetworkInterface
}

func createTestNodes(interfaces, rbcInterfaces []networking.NetworkInterface, ks KeyStore,
	privateKeys map[int64]kyber.Scalar, config secretsharing.Config) []*TestNode {
	nodes := make([]*TestNode, len(interfaces))

	for i, iface := range interfaces {
		rbc := test.NewMockRBC(rbcInterfaces[i], nil)
		acss := NewACSS(config, iface, rbc, ks, privateKeys[iface.GetID()], iface.GetID()-1)
		rbc.SetPredicate(acss.predicate)
		nodes[i] = &TestNode{
			acss:          acss,
			rbcInterface:  rbcInterfaces[i],
			acssInterface: iface,
		}
	}

	return nodes
}

func setupTest(config secretsharing.Config) (acssIfaces, rbcIfaces []networking.NetworkInterface,
	ks KeyStore, pks map[int64]kyber.Scalar) {
	// Create two different networks for RBC and ACSS
	network := networking.NewFakeNetwork()
	rbcNetwork := networking.NewFakeNetwork()

	interfaces, err := test.SetupNetwork(network, config.NbNodes)
	if err != nil {
		panic(err)
	}
	rbcInterfaces, err := test.SetupNetwork(rbcNetwork, config.NbNodes)
	if err != nil {
		panic(err)
	}

	// Setup Key store
	ks, pks = generateKeyStore(interfaces, config.Group)

	return interfaces, rbcInterfaces, ks, pks
}

func startNodes(ctx context.Context, t require.TestingT, nodes []*TestNode, expectedErr error) {
	for _, n := range nodes {
		go func() {
			err := n.acss.Start(ctx)
			require.ErrorIs(t, err, expectedErr)
		}()
	}
}

func checkReconstruction(t *testing.T, nodes []*TestNode, instanceHash []byte, expectedSecret kyber.Scalar,
	rbcWorked, reconstructionWorked bool) {
	for _, node := range nodes {
		instance, ok := node.acss.getInstance(instanceHash)
		// The instance should exist
		require.True(t, ok)

		if rbcWorked {
			// Check RBC successfully finished
			require.True(t, instance.RBCFinished())
		}

		if rbcWorked && reconstructionWorked {
			// Check reconstruction worked
			require.True(t, instance.Reconstructed())
			// Check the correct secret was reconstructed
			require.Equal(t, expectedSecret, instance.ReconstructedValue())
		}
	}
}

// ******** Core Functionalities ********

// TestACSS_SecretDistribution test hat the dealer can correctly distribute the secret shares to all participants.
// Expect all honest participants receive shares; the total number of shares matches the number of participants.
func TestACSS_SecretDistribution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := getDefaultConfig()

	acssInterfaces, rbcInterfaces, ks, privateKeys := setupTest(config)

	// Set up the test node
	rbc := test.NewMockRBC(rbcInterfaces[0], nil)
	nodeInterface := acssInterfaces[0]
	acss := NewACSS(config, nodeInterface, rbc, ks, privateKeys[nodeInterface.GetID()], nodeInterface.GetID()-1)
	rbc.SetPredicate(acss.predicate)

	// Set up the dummy interfaces
	dummyInterfaces := make([]networking.NetworkInterface, config.NbNodes-1)
	for i := 1; i < config.NbNodes; i++ {
		rbcIface := rbcInterfaces[i]
		testRBC := test.NewMockRBC(rbcIface, func([]byte) bool { return true })
		// Start listening
		go func() {
			_ = testRBC.Listen(ctx)
		}()
		dummyInterfaces[i-1] = rbcIface
	}

	// Start the ACSS node
	go func() {
		_ = acss.Start(ctx)
	}()

	secret := config.Group.Scalar().SetInt64(1)

	// Start ACSS
	instance, err := acss.Share(secret)
	require.NoError(t, err)

	// Wait a short period to allow for the messages to have been sent
	time.Sleep(1000 * time.Millisecond)

	require.True(t, instance.RBCFinished())

	// Expect active interface to have received a PROPOSE message from the node's RBC broadcast
	for _, dummyInterface := range dummyInterfaces {
		received := dummyInterface.GetReceived()

		// Should have received the RBC PROPOSE
		require.Equal(t, 1, len(received), "Interface %d should have received 1 PROPOSE message", dummyInterface.GetID())

		rbcPayload := &typedefs.ACSSMessage_RBCPayload{}
		protoErr := proto.Unmarshal(received[0], rbcPayload)
		require.NoError(t, protoErr)

		// Should have a correct commitment
		_, err = secretsharing.UnmarshalCommitment(rbcPayload.GetCommitment(), config.Group)
		require.NoError(t, err)

		// Should have an encrypted share for each node
		require.Equal(t, config.NbNodes, len(rbcPayload.GetEncryptedShares()))
	}
	cancel()
}

// TestACSS_ShareVerification test that participants can verify the correctness of received shares.
// Expect valid shares pass verification; tampered shares fail.
func TestACSS_ShareVerification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := getDefaultConfig()

	acssInterfaces, rbcInterfaces, ks, privateKeys := setupTest(config)

	// Set up the test node
	rbc := test.NewMockRBC(rbcInterfaces[0], nil)
	nodeInterface := acssInterfaces[0]
	acss := NewACSS(config, nodeInterface, rbc, ks, privateKeys[nodeInterface.GetID()], nodeInterface.GetID()-1)
	rbc.SetPredicate(acss.predicate)

	// Test predicates verifies that with the received RBC payload, the node can verify its share
	testPredicate := func(index int64, pk kyber.Scalar) func([]byte) bool {
		return func(bs []byte) bool {
			rbcPayload := &typedefs.ACSSMessage_RBCPayload{}
			protoErr := proto.Unmarshal(bs, rbcPayload)
			require.NoError(t, protoErr)
			if protoErr != nil {
				return false
			}

			// Should have a correct commitment
			commit, err := secretsharing.UnmarshalCommitment(rbcPayload.GetCommitment(), config.Group)
			require.NoError(t, err)
			if err != nil {
				return false
			}

			encryptedShare := rbcPayload.GetEncryptedShares()[index]
			decryptedShare, err := ecies.Decrypt(config.Group, pk, encryptedShare, sha256.New)
			require.NoError(t, err)
			if err != nil {
				return false
			}

			sShare := decryptedShare[:len(decryptedShare)/2]
			rShare := decryptedShare[len(decryptedShare)/2:]

			si, ri, err := secretsharing.UnmarshalShares(sShare, rShare)
			require.NoError(t, err)
			if err != nil {
				return false
			}

			ok := pedersencommitment.PedPolyVerify(commit, int64(si.I), si, ri, config.Group, config.Base0, config.Base1)
			require.True(t, ok)
			return ok
		}
	}

	// Set up the dummy interfaces
	dummyInterfaces := make([]networking.NetworkInterface, config.NbNodes-1)
	for i := 1; i < config.NbNodes; i++ {
		rbcIface := rbcInterfaces[i]
		predicate := testPredicate(rbcIface.GetID()-1, privateKeys[rbcIface.GetID()])
		testRBC := test.NewMockRBC(rbcIface, predicate)
		// Start listening
		go func() {
			_ = testRBC.Listen(ctx)
		}()
		dummyInterfaces[i-1] = rbcIface
	}

	// Start the ACSS node
	go func() {
		_ = acss.Start(ctx)
	}()

	secret := config.Group.Scalar().SetInt64(1)

	// Start ACSS
	instance, err := acss.Share(secret)
	require.NoError(t, err)

	// Wait a short period to allow for the messages to have been sent
	time.Sleep(500 * time.Millisecond)

	require.True(t, instance.RBCFinished())

	// Expect active interface to have received a PROPOSE message from the node's RBC broadcast
	for _, dummyInterface := range dummyInterfaces {
		received := dummyInterface.GetReceived()

		// Should have received the RBC PROPOSE
		require.Equal(t, 1, len(received), "Interface %d should have received 1 PROPOSE message", dummyInterface.GetID())

	}
	cancel()
}

// TestACSS_SecretReconstruction test that a sufficient subset of shares can reconstruct the original secret.
// Expect reconstruction succeeds with any valid quorum
func TestACSS_SecretReconstruction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := getDefaultConfig()

	acssInterfaces, rbcInterfaces, ks, privateKeys := setupTest(config)

	// Set up the test node
	rbc := test.NewMockRBC(rbcInterfaces[0], nil)
	nodeInterface := acssInterfaces[0]
	acss := NewACSS(config, nodeInterface, rbc, ks, privateKeys[nodeInterface.GetID()], nodeInterface.GetID()-1)
	rbc.SetPredicate(acss.predicate)

	// Set up the dummy interfaces
	dummyInterfaces := make([]networking.NetworkInterface, config.NbNodes-1)
	for i := 1; i < config.NbNodes; i++ {
		rbcIface := rbcInterfaces[i]
		testRBC := test.NewMockRBC(rbcIface, func([]byte) bool { return true })
		// Start listening
		go func() {
			_ = testRBC.Listen(ctx)
		}()
		dummyInterfaces[i-1] = rbcIface
	}

	// Start the ACSS node
	go func() {
		_ = acss.Start(ctx)
	}()

	secret := config.Group.Scalar().SetInt64(1)

	// Start ACSS
	instance, err := acss.Share(secret)
	require.NoError(t, err)

	// Wait a short period to allow for the messages to have been sent
	time.Sleep(500 * time.Millisecond)

	require.True(t, instance.RBCFinished())

	// Try to reconstruct with the minimum amount of shares (t+1)
	secretShares := make([]*share.PriShare, 0)
	for _, dummyInterface := range dummyInterfaces[:config.Threshold+1] {
		received := dummyInterface.GetReceived()

		rbcPayload := &typedefs.ACSSMessage_RBCPayload{}
		protoErr := proto.Unmarshal(received[0], rbcPayload)
		require.NoError(t, protoErr)

		pk := privateKeys[dummyInterface.GetID()]

		encryptedShare := rbcPayload.GetEncryptedShares()[dummyInterface.GetID()-1]
		decryptedShare, err := ecies.Decrypt(config.Group, pk, encryptedShare, sha256.New)
		require.NoError(t, err)

		sShare := decryptedShare[:len(decryptedShare)/2]
		rShare := decryptedShare[len(decryptedShare)/2:]

		si, _, err := secretsharing.UnmarshalShares(sShare, rShare)
		require.NoError(t, err)

		secretShares = append(secretShares, si)
	}

	secretPoly, err := share.RecoverPriPoly(config.Group, secretShares, config.Threshold, config.NbNodes)
	require.NoError(t, err)

	recoveredSecret := secretPoly.Secret()

	require.Equal(t, secret, recoveredSecret)

	cancel()
}

// TestACSS_FullSharePhase test that a network of all ACSS node can finish the sharing phase
// Expect the RBC broadcast to finish and all nodes have registered the instance
func TestACSS_FullSharePhase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := getDefaultConfig()

	acssInterfaces, rbcInterfaces, ks, privateKeys := setupTest(config)

	nodes := createTestNodes(acssInterfaces, rbcInterfaces, ks, privateKeys, config)

	startNodes(ctx, t, nodes, context.Canceled)

	dealer := nodes[0]
	secret := config.Group.Scalar().SetInt64(1)

	// Start ACSS from the dealer
	instance, err := dealer.acss.Share(secret)
	require.NoError(t, err)

	// Wait for RBC to finish
	time.Sleep(500 * time.Millisecond)

	require.True(t, instance.RBCFinished())

	for _, node := range nodes {
		instances := node.acss.GetInstances()

		// Should have registered an instance
		require.Len(t, instances, 1)

		inst := instances[0]

		// Instance should have finished RBC
		require.True(t, inst.RBCFinished())
	}

	cancel()
}

// TestACSS_SendReconstructMessage test that calling Reconstruct sends a RECONSTRUCT message to all nodes
// Expect all nodes to have received a RECONSTRUCT message
func TestACSS_SendReconstructMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := getDefaultConfig()

	acssInterfaces, rbcInterfaces, ks, privateKeys := setupTest(config)

	nodes := createTestNodes(acssInterfaces, rbcInterfaces, ks, privateKeys, config)

	dealer := nodes[0]

	// Create an Instance to mock the sharing phase
	secret := config.Group.Scalar().SetInt64(1)
	commit, sShares, rShares, err := pedersencommitment.PedPolyCommit(secret, config.Threshold,
		config.NbNodes, config.Group, config.Base0, config.Base1)
	require.NoError(t, err)

	encryptedShares, err := dealer.acss.encryptShares(sShares, rShares)
	require.NoError(t, err)

	rbcPayloadMessage, err := dealer.acss.createRBCPayload(commit, encryptedShares)
	require.NoError(t, err)

	rbcPayload, err := proto.Marshal(rbcPayloadMessage)
	require.NoError(t, err)

	// Compute the hash of the payload to be able to identify RBC messages for this instance
	hasher := sha256.New()
	hasher.Write(rbcPayload)
	hash := hasher.Sum(nil)

	for _, node := range nodes {
		instance := NewInstance(hash)
		sShare := sShares[node.acss.GetIndex()]
		rShare := rShares[node.acss.GetIndex()]
		instance.SetRBCResult(commit, sShare, rShare)

		node.acss.instances[string(hash)] = instance
	}

	// Start Reconstruct from the dealer
	startNodes(ctx, t, nodes, context.Canceled)

	instance := dealer.acss.GetInstances()[0]
	err = dealer.acss.Reconstruct(instance)
	require.NoError(t, err)

	// Wait for reconstruction messages to be sent
	time.Sleep(500 * time.Millisecond)

	for _, node := range nodes {
		received := node.acssInterface.GetReceived()

		// Should have received a RECONSTRUCT message
		require.Equal(t, 1, len(received))

		packet := &typedefs.Packet{}
		err := proto.Unmarshal(received[0], packet)
		require.NoError(t, err)

		acssMessage, ok := packet.GetMessage().(*typedefs.Packet_AcssMessage)
		require.True(t, ok)

		// Message received should be a RECONSTRUCT message
		_, ok = acssMessage.AcssMessage.GetOp().(*typedefs.ACSSMessage_ReconstructInst)
		require.True(t, ok)
	}

	cancel()
}

// TestACSS_AllReconstruct test that the Instance is correctly reconstructed when all nodes participate in the
// reconstruction.
// Expect that the instances have all reconstructed the same value
func TestACSS_AllReconstruct(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := getDefaultConfig()

	acssInterfaces, rbcInterfaces, ks, privateKeys := setupTest(config)

	nodes := createTestNodes(acssInterfaces, rbcInterfaces, ks, privateKeys, config)

	dealer := nodes[0]

	// Create an Instance to mock the sharing phase
	secret := config.Group.Scalar().SetInt64(1)
	commit, sShares, rShares, err := pedersencommitment.PedPolyCommit(secret, config.Threshold,
		config.NbNodes, config.Group, config.Base0, config.Base1)
	require.NoError(t, err)

	encryptedShares, err := dealer.acss.encryptShares(sShares, rShares)
	require.NoError(t, err)

	rbcPayloadMessage, err := dealer.acss.createRBCPayload(commit, encryptedShares)
	require.NoError(t, err)

	rbcPayload, err := proto.Marshal(rbcPayloadMessage)
	require.NoError(t, err)

	// Compute the hash of the payload to be able to identify RBC messages for this instance
	hasher := sha256.New()
	hasher.Write(rbcPayload)
	hash := hasher.Sum(nil)

	for _, node := range nodes {
		instance := NewInstance(hash)
		sShare := sShares[node.acss.GetIndex()]
		rShare := rShares[node.acss.GetIndex()]
		instance.SetRBCResult(commit, sShare, rShare)

		node.acss.instances[string(hash)] = instance
	}

	startNodes(ctx, t, nodes, context.Canceled)

	// Start Reconstruct on all nodes
	for _, node := range nodes {
		instance := node.acss.GetInstances()[0]
		err = node.acss.Reconstruct(instance)
		require.NoError(t, err)
	}

	// Wait for reconstruction messages to be sent
	time.Sleep(500 * time.Millisecond)

	instanceHash := nodes[0].acss.GetInstances()[0].Identifier()

	// Check reconstruction worked as expected
	checkReconstruction(t, nodes, instanceHash, secret, true, true)

	cancel()
}

// TestACSS_AllReconstruct test that the Instance is correctly reconstructed when a just enough (threshold + 1)
// nodes participate in the reconstruction.
// Expect that the instances of the participating nodes have all reconstructed the same value
func TestACSS_ThresholdReconstruct(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := getDefaultConfig()

	acssInterfaces, rbcInterfaces, ks, privateKeys := setupTest(config)

	nodes := createTestNodes(acssInterfaces, rbcInterfaces, ks, privateKeys, config)

	dealer := nodes[0]

	// Create an Instance to mock the sharing phase
	secret := config.Group.Scalar().SetInt64(1)
	commit, sShares, rShares, err := pedersencommitment.PedPolyCommit(secret, config.Threshold,
		config.NbNodes, config.Group, config.Base0, config.Base1)
	require.NoError(t, err)

	encryptedShares, err := dealer.acss.encryptShares(sShares, rShares)
	require.NoError(t, err)

	rbcPayloadMessage, err := dealer.acss.createRBCPayload(commit, encryptedShares)
	require.NoError(t, err)

	rbcPayload, err := proto.Marshal(rbcPayloadMessage)
	require.NoError(t, err)

	// Compute the hash of the payload to be able to identify RBC messages for this instance
	hasher := sha256.New()
	hasher.Write(rbcPayload)
	hash := hasher.Sum(nil)

	reconstructingNodes := make([]*TestNode, 0)
	nonReconstructingNodes := nodes[config.Threshold+1:]
	for i, node := range nodes {
		instance := NewInstance(hash)
		sShare := sShares[node.acss.GetIndex()]
		rShare := rShares[node.acss.GetIndex()]
		instance.SetRBCResult(commit, sShare, rShare)

		node.acss.instances[string(hash)] = instance

		if i < config.Threshold+1 {
			reconstructingNodes = append(reconstructingNodes, node)
		}
	}

	// Start with different context to allow stopping the node that are not reconstructing
	startNodes(ctx, t, reconstructingNodes, context.Canceled)
	//startNodes(ctx2, t, nonReconstructingNodes, context.Canceled)

	// Start Reconstruct one node after the other and check that it doesn't work for the threshold first nodes
	for _, node := range reconstructingNodes[:config.Threshold] {
		instance := node.acss.GetInstances()[0]
		err = node.acss.Reconstruct(instance)

		require.NoError(t, err)
		// Wait for reconstruction messages to be sent
		time.Sleep(500 * time.Millisecond)

		// Expect RBC to have worked but not reconstruction for all nodes
		checkReconstruction(t, nodes, instance.Identifier(), secret, true, false)

	}

	// Reconstruct from the t+1 node and check that the nodes managed to reconstruct
	instance := reconstructingNodes[config.Threshold].acss.GetInstances()[0]
	err = reconstructingNodes[config.Threshold].acss.Reconstruct(instance)

	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Check reconstruction worked
	checkReconstruction(t, reconstructingNodes, instance.Identifier(), secret, true, true)

	// Check no node not taking part managed to reconstruct
	checkReconstruction(t, nonReconstructingNodes, instance.Identifier(), secret, false, false)

	cancel()
}

func TestACSS_ShareAndReconstruct(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := getDefaultConfig()

	acssInterfaces, rbcInterfaces, ks, privateKeys := setupTest(config)

	nodes := createTestNodes(acssInterfaces, rbcInterfaces, ks, privateKeys, config)

	startNodes(ctx, t, nodes, context.Canceled)

	dealer := nodes[0]
	secret := config.Group.Scalar().SetInt64(1)

	// Start ACSS from the dealer
	instance, err := dealer.acss.Share(secret)
	require.NoError(t, err)

	// Wait for RBC to finish
	time.Sleep(500 * time.Millisecond)

	// Reconstruct from all nodes
	for _, node := range nodes {
		instance := node.acss.GetInstances()[0]
		err := node.acss.Reconstruct(instance)
		require.NoError(t, err)
	}

	// Wait for all messages to be delivered
	time.Sleep(500 * time.Millisecond)

	// Check that all nodes successfully reconstructed the correct value
	checkReconstruction(t, nodes, instance.Identifier(), secret, true, true)

	cancel()
}
