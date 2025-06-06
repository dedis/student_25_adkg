package networking

import (
	"context"
	"errors"
	"student_25_adkg/logging"
	"student_25_adkg/typedefs"
	"sync"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

type Callback func(*typedefs.Packet) error

type Node struct {
	iface    NetworkInterface
	callback Callback
	logger   zerolog.Logger
	sync.Mutex
}

func NewNode(iface NetworkInterface) *Node {
	return &Node{
		iface:  iface,
		logger: logging.GetLogger(iface.GetID()),
	}
}

func (n *Node) Start(ctx context.Context) error {
	for {
		bs, err := n.iface.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
		}

		err = n.handleMessage(bs)
		if err != nil {
			n.logger.Error().Err(err).Msg("failed to handle message")
		}

	}
}

func (n *Node) handleMessage(message []byte) error {
	packet := &typedefs.Packet{}
	err := proto.Unmarshal(message, packet)
	if err != nil {
		return err
	}
	return n.callback(packet)
}

func (n *Node) SetCallback(callback Callback) {
	n.Lock()
	defer n.Unlock()
	n.callback = callback
}

func (n *Node) Broadcast(packet *typedefs.Packet) error {
	bs, err := proto.Marshal(packet)
	if err != nil {
		return err
	}
	return n.iface.Broadcast(bs)
}
