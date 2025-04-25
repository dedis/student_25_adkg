package networking

import (
	"context"
	"fmt"
	"sync"
)

// FakeNetwork is a structure implementing a network. It connects the interfaces of all nodes. T is the type of the
// messages
type FakeNetwork[T any] struct {
	nodes map[int64]chan T
	in    chan T
}

func NewFakeNetwork[T any]() *FakeNetwork[T] {
	return &FakeNetwork[T]{
		nodes: make(map[int64]chan T),
		in:    make(chan T, 500),
	}
}

func (n *FakeNetwork[T]) freshID() int64 {
	return int64(len(n.nodes) + 1)
}

func (n *FakeNetwork[T]) JoinWithBuffer(size int) *FakeInterface[T] {
	queue := make(chan T, size)
	iface := NewFakeInterface[T](queue, n.Send, n.Broadcast, n.freshID())

	n.nodes[iface.id] = iface.rcvQueue

	return iface
}

func (n *FakeNetwork[T]) JoinNetwork() *FakeInterface[T] {
	return n.JoinWithBuffer(100)
}

func (n *FakeNetwork[T]) Send(msg T, to int64) error {
	rcv, ok := n.nodes[to]
	if !ok {
		return fmt.Errorf("destination node %d not found", to)
	}
	// Put the message in the recipient's receive channel
	rcv <- msg
	return nil
}

func (n *FakeNetwork[T]) Broadcast(msg T) error {
	for i := range n.nodes {
		err := n.Send(msg, i)
		if err != nil {
			return err
		}
	}
	return nil
}

// NetworkInterface represents an interface used by a node to communicate in the network
type NetworkInterface[T any] interface {
	// Send allows to send a byte message to a recipient addressed by an int
	Send(T, int64) error
	// Broadcast send the given byte message to everyone else in the network
	Broadcast(T) error
	// Receive waits on the channel for a message to arrive. Blocks until a message arrives or
	// the channel is written to. This allows stopping before receiving a message
	Receive(context.Context) (T, error)
	GetID() int64
	GetSent() []T
	GetReceived() []T
}
type FakeInterface[T any] struct {
	rcvQueue     chan T
	sendMsg      func(T, int64) error
	broadcastMsg func(T) error
	id           int64
	received     []T
	sent         []T
	sync.RWMutex
}

func NewFakeInterface[T any](rcv chan T, sendMsg func(T, int64) error,
	broadcastMsg func(T) error, id int64) *FakeInterface[T] {
	return &FakeInterface[T]{
		rcvQueue:     rcv,
		sendMsg:      sendMsg,
		broadcastMsg: broadcastMsg,
		id:           id,
		received:     make([]T, 0),
		sent:         make([]T, 0),
	}
}

func (f *FakeInterface[T]) Send(msg T, to int64) error {
	f.Lock()
	defer f.Unlock()
	err := f.sendMsg(msg, to)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *FakeInterface[T]) Broadcast(msg T) error {
	f.Lock()
	defer f.Unlock()
	err := f.broadcastMsg(msg)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *FakeInterface[T]) Receive(ctx context.Context) (T, error) {
	select {
	case msg := <-f.rcvQueue:
		f.Lock()
		defer f.Unlock()
		f.received = append(f.received, msg)
		return msg, nil
	case <-ctx.Done():
		var msg T
		return msg, ctx.Err()
	}
}

func (f *FakeInterface[T]) GetID() int64 {
	return f.id
}

func (f *FakeInterface[T]) GetSent() []T {
	f.RLock()
	defer f.RUnlock()
	return f.sent
}

func (f *FakeInterface[T]) GetReceived() []T {
	f.RLock()
	defer f.RUnlock()
	return f.received
}
