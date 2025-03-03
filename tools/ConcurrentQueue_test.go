package tools

import (
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
)

// Simple test to ensure an single element is added correctly
func TestConcurrentQueue_Add(t *testing.T) {
	q := NewConcurrentQueue[[]byte](10)
	msg := []byte("Hello World")
	err := q.Push(msg)
	require.NoError(t, err)
	require.Equal(t, q.queue[q.head], msg)
	require.Equal(t, q.head+1, q.tail)
}

// Simple test to ensure that an element is popped correctly
func TestConcurrentQueue_Pop(t *testing.T) {
	// Push a dummy message
	q := NewConcurrentQueue[[]byte](10)
	msg := []byte("Hello World")
	err := q.Push(msg)
	require.NoError(t, err)

	// Pop the message
	popped, err := q.Pop()
	require.NoError(t, err)
	require.Equal(t, msg, popped)
	require.True(t, q.IsEmpty())

	popped, err = q.Pop()
	require.Error(t, err)
}

// Ensures that adding an element to a full queue returns an error
func TestConcurrentQueue_Max_Capacity(t *testing.T) {
	q := NewConcurrentQueue[[]byte](2)

	msg := []byte("Hello World")

	err := q.Push(msg)
	require.NoError(t, err)
	err = q.Push(msg)
	require.NoError(t, err)

	// Try to add a new message in the full queue
	err = q.Push(msg)
	require.Error(t, err)
}

// Check that the order in which the messages are returned when calling Pop matches
// the order in which they ar
func TestConcurrentQueue_Correct_Order(t *testing.T) {
	q := NewConcurrentQueue[[]byte](5)
	nbMessages := 5
	messages := make([][]byte, nbMessages)
	for i := 0; i < nbMessages; i++ {
		messages[i] = []byte("Hello World" + strconv.Itoa(i))
		err := q.Push(messages[i])
		require.NoError(t, err)
	}

	index := 0
	for !q.IsEmpty() {
		msg, err := q.Pop()
		require.NoError(t, err)
		require.Equal(t, messages[index], msg)
		index++
	}
}
