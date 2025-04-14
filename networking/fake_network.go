package networking

import (
	"fmt"
)

// FakeNetwork is a structure implementing a network. It connects the interfaces of all nodes. T is the type of the
// messages
type FakeNetwork[T any] struct {
	nodes map[int]chan T
	in    chan T
}

func NewFakeNetwork[T any]() *FakeNetwork[T] {
	return &FakeNetwork[T]{
		nodes: make(map[int]chan T),
		in:    make(chan T, 100),
	}
}

func (n *FakeNetwork[T]) freshID() uint32 {
	return uint32(len(n.nodes) + 1)
}

func (n *FakeNetwork[T]) JoinNetwork() *FakeInterface[T] {
	queue := make(chan T, 100)
	iface := NewFakeInterface[T](queue, n.Send, n.Broadcast, n.freshID())

	n.nodes[int(iface.id)] = iface.rcvQueue

	return iface
}

func (n *FakeNetwork[T]) Send(msg T, to uint32) error {
	rcv, ok := n.nodes[int(to)]
	if !ok {
		return fmt.Errorf("destination node %d not found", to)
	}
	// Put the message in the recipient's receive channel
	rcv <- msg
	return nil
}

func (n *FakeNetwork[T]) Broadcast(msg T) error {
	for i, _ := range n.nodes {
		err := n.Send(msg, uint32(i))
		if err != nil {
			return err
		}
	}
	return nil
}

// NetworkInterface represents an interface used by a node to communicate in the network
type NetworkInterface[T any] interface {
	// Send allows to send a byte message to a recipient addressed by an int
	Send(T, uint32) error
	// Broadcast send the given byte message to everyone else in the network
	Broadcast(T) error
	// Receive waits on the channel for a message to arrive. Blocks until a message arrives
	Receive() (T, error)
	GetID() uint32
}

type FakeInterface[T any] struct {
	rcvQueue     chan T
	sendMsg      func(T, uint32) error
	broadcastMsg func(T) error
	id           uint32
	received     []T
	sent         []T
}

func NewFakeInterface[T any](rcv chan T, sendMsg func(T, uint32) error,
	broadcastMsg func(T) error, id uint32) *FakeInterface[T] {
	return &FakeInterface[T]{
		rcvQueue:     rcv,
		sendMsg:      sendMsg,
		broadcastMsg: broadcastMsg,
		id:           id,
		received:     make([]T, 0),
		sent:         make([]T, 0),
	}
}

func (f *FakeInterface[T]) Send(msg T, to uint32) error {
	err := f.sendMsg(msg, to)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *FakeInterface[T]) Broadcast(msg T) error {
	err := f.broadcastMsg(msg)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *FakeInterface[T]) Receive() (T, error) {
	val := <-f.rcvQueue
	f.received = append(f.received, val)
	return val, nil
}

func (f *FakeInterface[T]) GetID() uint32 {
	return f.id
}

func (f *FakeInterface[T]) GetSent() []T {
	return f.sent
}

func (f *FakeInterface[T]) GetReceived() []T {
	return f.received
}
