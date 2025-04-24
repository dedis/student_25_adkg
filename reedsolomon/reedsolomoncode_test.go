package reedsolomon

import (
	"github.com/HACKERALERT/infectious"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/group/edwards25519"
	"testing"
)

func checkEncoding(t *testing.T, encoded []infectious.Share, expected []byte, decoder RSDecoder) {
	// Decode the message
	decoded, err := decoder.Decode(encoded)
	require.NoError(t, err)

	// Check that the decoded message is the same as the original
	require.Equal(t, len(expected), len(decoded))
	for i, scalar := range decoded {
		exp := expected[i]
		require.Equal(t, exp, scalar)
	}
}

// TestRSCode_Simple tests that encoding and decoding a message without any errors in the message to decode
// works.
func TestRSCode_Simple(t *testing.T) {
	k := 3
	n := 7
	rsCodes := NewBWCodes(k, n)

	// Create a message with values  from 0 to k-1
	message := make([]byte, k)
	for i := 0; i < k; i++ {
		message[i] = byte(i)
	}

	// Encode the message
	encoded, err := rsCodes.Encode(message)
	require.NoError(t, err)

	checkEncoding(t, encoded, message, rsCodes)
}

// TestRSCode_Corrupted tests that encoding and decoding a message where some
// data corrupted still works
func TestRSCode_Corrupted(t *testing.T) {
	k := 3
	n := 7
	rsCodes := NewBWCodes(k, n)
	// Max error is lower bound of (n-k)/2

	// Create a message with values  from 0 to k-1
	message := make([]byte, 2*k)
	for i := 0; i < 2*k; i++ {
		message[i] = byte(i)
	}

	// Encode the message
	encoded, err := rsCodes.Encode(message)
	require.NoError(t, err)

	// Alter some data
	encoded[1].Data[0] = byte('s')
	encoded[1].Data[1] = byte('e')
	encoded[0].Data[0] = byte('x')
	encoded[0].Data[1] = byte('y')

	// Check the encoding
	checkEncoding(t, encoded, message, rsCodes)

	// Add additional corruption and check that it fails (the call to decode fixed the "encoded" array)
	encoded[1].Data[0] = byte('s')
	encoded[1].Data[1] = byte('e')
	encoded[0].Data[0] = byte('x')
	encoded[0].Data[1] = byte('y')
	encoded[2].Data[0] = byte('!')

	// Decode the message
	_, err = rsCodes.Decode(encoded)
	require.Equal(t, infectious.TooManyErrors, err)
}

// TestRSCode_Erasure tests that encoding and decoding a message where some
// data got erased
func TestRSCode_Erasure(t *testing.T) {
	k := 3
	n := 7
	rsCodes := NewBWCodes(k, n)
	// Max error is lower bound of (n-k)/2

	// Create a message with values  from 0 to k-1
	message := make([]byte, 2*k)
	for i := 0; i < 2*k; i++ {
		message[i] = byte(i)
	}

	// Encode the message
	encoded, err := rsCodes.Encode(message)
	require.NoError(t, err)

	// Drop some data
	encoded = encoded[2:]

	// Check the encoding
	checkEncoding(t, encoded, message, rsCodes)

	// Drop 2 more packets

	encoded = encoded[2:]

	checkEncoding(t, encoded, message, rsCodes)

	// Drop one more packet and expect it to fail
	encoded = encoded[1:]

	// Decode the message
	_, err = rsCodes.Decode(encoded)
	require.Error(t, err)
}

// TestBWCodes_EncodeScalars tries to encode a list of kyber.Scalar and check that it
// works
func TestBWCodes_EncodeScalars(t *testing.T) {
	g := edwards25519.NewBlakeSHA256Ed25519()

	k := 3
	n := 7
	rsCodes := NewBWCodes(k, n)

	sSize := g.Scalar().MarshalSize()

	scalars := make([]kyber.Scalar, k)
	msg := make([]byte, 0)
	for i := 0; i < k; i++ {
		scalars[i] = g.Scalar().SetInt64(int64(i))
		b, err := scalars[i].MarshalBinary()
		require.NoError(t, err)
		msg = append(msg, b...)
	}

	encoded, err := rsCodes.Encode(msg)
	require.NoError(t, err)

	checkEncoding(t, encoded, msg, rsCodes)

	decoded, err := rsCodes.Decode(encoded)
	require.NoError(t, err)
	require.Equal(t, sSize*k, len(decoded))
	// Parse scalars and check with the original result

	for i := 0; i < k; i++ {
		bs := decoded[i*sSize : (i+1)*sSize]
		s := g.Scalar()
		err = s.UnmarshalBinary(bs)
		require.NoError(t, err)
		require.True(t, s.Equal(scalars[i]))
	}
}
