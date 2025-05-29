package acss

import (
	"student_25_adkg/networking"
	"student_25_adkg/secretsharing"
	test "student_25_adkg/testing"
	"student_25_adkg/typedefs"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"google.golang.org/protobuf/proto"
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

// ******** Core Functionalities ********

// TestACSS_SecretDistribution test hat the dealer can correctly distribute the secret shares to all participants.
// Expect all honest participants receive shares; the total number of shares matches the number of participants.
func TestACSS_SecretDistribution(t *testing.T) {
	network := networking.NewFakeNetwork()

	config := getDefaultConfig()

	interfaces, err := test.SetupNetwork(network, config.NbNodes)
	require.NoError(t, err)

	node := NewACSS(interfaces[0])

	secret := config.Group.Scalar().SetInt64(1)

	err = node.Share(secret)
	require.NoError(t, err)

	// Expect all interfaces to have received a shares message from the node
	for _, iface := range interfaces {
		received := iface.GetReceived()

		if iface.GetID() == node.iface.GetID() {
			continue
		}

		// All nodes should have received the RBC PROPOSE message with
		require.Equal(t, 1, len(received))

		packet := &typedefs.Packet{}
		protoErr := proto.Unmarshal(received[0], packet)
		require.NoError(t, protoErr)

		rbcMessage, ok := packet.GetMessage().(*typedefs.Packet_RbcMessage)
		require.True(t, ok)

		proposeMessage, ok := rbcMessage.RbcMessage.GetOp().(*typedefs.RBCMessage_ProposeInst)
		require.True(t, ok)

		_ = proposeMessage.ProposeInst.GetContent()
		// TODO check the data received
	}

}

// TestACSS_ShareVerification test that participants can verify the correctness of received shares (e.g., using commitments).
// Expect valid shares pass verification; tampered shares fail.
func TestACSS_ShareVerification(t *testing.T) {
	require.Fail(t, "TODO")
}

// TestACSS_SecretReconstruction test that a sufficient subset of shares can reconstruct the original secret.
// Expect reconstruction succeeds with any valid quorum
func TestACSS_SecretReconstruction(t *testing.T) {
	require.Fail(t, "TODO")
}

// TestACSS_InsufficientShares test the behavior when trying to reconstruct with fewer than the threshold number of shares.
// Expect reconstruction to fail or return an error.
func TestACSS_InsufficientShares(t *testing.T) {
	require.Fail(t, "TODO")
}

// ******** Asynchronous & Fault Tolerance ********

// TestACSS_WithDelayedMessages test that the protocol handles out-of-order or delayed messages gracefully.
// Expect that the secret is still correctly reconstructed.
func TestACSS_WithDelayedMessages(t *testing.T) {
	require.Fail(t, "TODO")
}

// TestACSS_WithDroppedMessages test that the protocol is robust to dropped messages (some shares never arrive).
// Expect that as long as the threshold is met, reconstruction still works.
func TestACSS_WithDroppedMessages(t *testing.T) {
	require.Fail(t, "TODO")
}

// TestACSS_WithByzantineParticipants test the behavior when some participants send incorrect or malformed shares.
// Expect that the malicious shares are detected and ignored; reconstruction still succeeds if quorum is honest.
func TestACSS_WithByzantineParticipants(t *testing.T) {
	require.Fail(t, "TODO")
}

// ******** Security & Consistency ********

// TestACSS_ShareUniqueness test that each participant receives a unique share.
// Expect that no two participants receive the same share.
func TestACSS_ShareUniqueness(t *testing.T) {
	require.Fail(t, "TODO")
}

// TestACSS_ConsistencyAmongParticipants test that all honest participants reconstruct the same secret.
// Expect an identical output among all honest nodes.
func TestACSS_ConsistencyAmongParticipants(t *testing.T) {
	require.Fail(t, "TODO")
}

// ******** Edge Cases & Limits ********

// TestACSS_EmptySecret test the behavior when trying to share an empty secret.
// Expect that the protocol handles it without crashing, possibly erroring gracefully.
func TestACSS_EmptySecret(t *testing.T) {
	require.Fail(t, "TODO")
}

// TestACSS_Stress test the behavior when running with a large network of users.
// Expect that the algorithm still performs correctly under load
func TestACSS_Stress(t *testing.T) {
	require.Fail(t, "TODO")
}

// TestACSS_InvalidParameters test the rejection of invalid inputs (e.g., threshold > number of participants).
// Expect a proper error handling or panic with a clear message.
func TestACSS_InvalidParameters(t *testing.T) {
	require.Fail(t, "TODO")
}
