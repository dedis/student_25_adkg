package networking

import (
	"fmt"
	"student_25_adkg/tools"
)

// FakeNetwork is a structure implementing a network. It connects the interfaces of all nodes. T is the type of the
// messages
type FakeNetwork[T any] struct {
	nodes map[int]tools.Queue[T]
	in    tools.Queue[T]
}

func NewFakeNetwork[T any]() *FakeNetwork[T] {
	return &FakeNetwork[T]{
		nodes: make(map[int]tools.Queue[T]),
		in:    tools.NewConcurrentQueue[T](50),
	}
}

func (n *FakeNetwork[T]) freshID() int {
	return len(n.nodes) + 1
}

func (n *FakeNetwork[T]) JoinNetwork() *FakeInterface[T] {
	queue := tools.NewConcurrentQueue[T](10)
	iface := NewFakeInterface[T](queue, n.Send, n.Broadcast, n.freshID())

	n.nodes[iface.id] = iface.rcvQueue

	return iface
}

func (n *FakeNetwork[T]) Send(msg T, to int) error {
	rcv, ok := n.nodes[to]
	if !ok {
		return fmt.Errorf("destination node %d not found", to)
	}
	// Put the message in the recipient's receive channel
	return rcv.Push(msg)
}

func (n *FakeNetwork[T]) Broadcast(msg T) error {
	for i, _ := range n.nodes {
		err := n.Send(msg, i)
		if err != nil {
			return err
		}
	}
	return nil
}

// NetworkInterface represents an interface used by a node to communicate in the network
type NetworkInterface interface {
	// Send allows to send a byte message to a recipient addressed by an int
	Send([]byte, int) error
	// Broadcast send the given byte message to everyone else in the network
	Broadcast([]byte) error
	// HasMessage checks if the reception channel contains a message
	HasMessage() bool
	// Receive dequeues message received, nil if there are no messages pending
	Receive() ([]byte, error)
}

type FakeInterface[T any] struct {
	rcvQueue     tools.Queue[T]
	sendMsg      func(T, int) error
	broadcastMsg func(T) error
	id           int
}

func NewFakeInterface[T any](rcv tools.Queue[T], sendMsg func(T, int) error,
	broadcastMsg func(T) error, id int) *FakeInterface[T] {
	return &FakeInterface[T]{
		rcvQueue:     rcv,
		sendMsg:      sendMsg,
		broadcastMsg: broadcastMsg,
		id:           id,
	}
}

func (f *FakeInterface[T]) Send(msg T, to int) error {
	return f.sendMsg(msg, to)
}

func (f *FakeInterface[T]) Broadcast(msg T) error {
	return f.broadcastMsg(msg)
}

func (f *FakeInterface[T]) HasMessage() bool {
	return !f.rcvQueue.IsEmpty()
}

func (f *FakeInterface[T]) Receive() (T, error) {
	return f.rcvQueue.Pop()
}
