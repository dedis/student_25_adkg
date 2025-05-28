package networking

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FakeNetwork is a structure implementing a network. It connects the interfaces of all nodes. T is the type of the
// messages
type FakeNetwork struct {
	nodes    map[int64]chan []byte
	delayMap map[int64]time.Duration
	in       chan []byte
}

func NewFakeNetwork() *FakeNetwork {
	return &FakeNetwork{
		nodes:    make(map[int64]chan []byte),
		in:       make(chan []byte, 50000),
		delayMap: make(map[int64]time.Duration),
	}
}

// DelayNode adds the given delay to the node when sending a packet.
// Mimics a node having slow connection
func (n *FakeNetwork) DelayNode(id int64, delay time.Duration) {
	n.delayMap[id] = delay
}

func (n *FakeNetwork) freshID() int64 {
	return int64(len(n.nodes) + 1)
}

func (n *FakeNetwork) JoinWithBuffer(size int) (*FakeInterface, error) {
	queue := make(chan []byte, size)
	iface := NewFakeInterface(queue, n.Send, n.Broadcast, n.freshID())

	n.nodes[iface.id] = iface.rcvQueue

	return iface, nil
}

func (n *FakeNetwork) JoinNetwork() (NetworkInterface, error) {
	return n.JoinWithBuffer(10000)
}

func (n *FakeNetwork) Send(msg []byte, from, to int64) error {
	rcv, ok := n.nodes[to]
	if !ok {
		return fmt.Errorf("destination node %d not found", to)
	}
	// Put the message in the recipient's receive channel
	delay, ok := n.delayMap[from]
	if ok {
		go func() {
			time.Sleep(delay)
			rcv <- msg
		}()
	} else {
		rcv <- msg
	}
	return nil
}

func (n *FakeNetwork) Broadcast(msg []byte, from int64) error {
	for i := range n.nodes {
		err := n.Send(msg, from, i)
		if err != nil {
			return err
		}
	}
	return nil
}

type FakeInterface struct {
	rcvQueue     chan []byte
	sendMsg      func([]byte, int64, int64) error
	broadcastMsg func([]byte, int64) error
	id           int64
	received     [][]byte
	sent         [][]byte
	sync.RWMutex
}

func NewFakeInterface(rcv chan []byte, sendMsg func([]byte, int64, int64) error,
	broadcastMsg func([]byte, int64) error, id int64) *FakeInterface {
	return &FakeInterface{
		rcvQueue:     rcv,
		sendMsg:      sendMsg,
		broadcastMsg: broadcastMsg,
		id:           id,
		received:     make([][]byte, 0),
		sent:         make([][]byte, 0),
	}
}

func (f *FakeInterface) Send(msg []byte, to int64) error {
	f.Lock()
	defer f.Unlock()
	err := f.sendMsg(msg, f.id, to)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *FakeInterface) Broadcast(msg []byte) error {
	f.Lock()
	defer f.Unlock()
	err := f.broadcastMsg(msg, f.id)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *FakeInterface) Receive(ctx context.Context) ([]byte, error) {
	select {
	case msg := <-f.rcvQueue:
		f.Lock()
		defer f.Unlock()
		f.received = append(f.received, msg)
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *FakeInterface) GetID() int64 {
	return f.id
}

func (f *FakeInterface) GetSent() [][]byte {
	f.RLock()
	defer f.RUnlock()
	duplicate := make([][]byte, len(f.sent))
	for i, msg := range f.sent {
		duplicate[i] = make([]byte, len(msg))
		copy(duplicate[i], msg)
	}
	return duplicate
}

func (f *FakeInterface) GetReceived() [][]byte {
	f.RLock()
	defer f.RUnlock()
	duplicate := make([][]byte, len(f.received))
	for i, msg := range f.received {
		duplicate[i] = make([]byte, len(msg))
		copy(duplicate[i], msg)
	}
	return duplicate
}
