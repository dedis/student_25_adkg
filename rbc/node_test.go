package rbc

import (
	"context"
	"student_25_adkg/networking"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNode_GetIdentifier(t *testing.T) {
	node := NewNode(nil, int64(0))
	require.Equal(t, int64(0), node.GetIdentifier())
}

func TestNode_Simple(t *testing.T) {

	network := networking.NewFakeNetwork()

	iface, err := network.JoinNetwork()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	node := NewNode(iface, int64(0))

	go func() {
		require.NoError(t, node.Start(ctx))
	}()

	message := []byte("hello world")

	err = node.BroadcastBytes(message)
	require.NoError(t, err)

	cancel()
}
