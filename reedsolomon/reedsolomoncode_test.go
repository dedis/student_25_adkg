package reedsolomon

import (
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"testing"
)

// TestRSCodeSimple tests that encoding and decoding a message without any errors in the message to decode
// works. It is a kind of sanity test.
func TestRSCodeSimple(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()
	rscodes := NewRSCodes(g)

	k := 5
	n := 6
	// Create a message with values  from 0 to k-1
	message := make([]kyber.Scalar, k)
	for i := 0; i < k; i++ {
		message[i] = g.Scalar().SetInt64(int64(i))
	}

	// Encode the message
	encoded, err := rscodes.Encode(message, n)
	require.NoError(t, err)

	// Decode the message
	decoded, err := rscodes.Decode(encoded, k)
	require.NoError(t, err)

	// Check that the decoded message is the same as the original
	require.Equal(t, len(message), len(decoded))
	require.Equal(t, len(decoded), k)
	for i, scalar := range decoded {
		exp := message[i]
		require.Equal(t, exp, scalar)
	}
}
