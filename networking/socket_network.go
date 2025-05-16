package networking

// Implementation of the network interface
// adapted to work on top of dedis/cs438 transport layer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"student_25_adkg/transport"
)

type TransportNetwork struct {
	mu        sync.Mutex
	transport transport.Transport
	nextID    int64
	nodes     map[int64]string // id -> address
	sockets   map[int64]transport.Socket
	peers     *PeerMap
}

// NewNetwork creates a new network instance using the given transport
func NewTransportNetwork(t transport.Transport) *TransportNetwork {
	return &TransportNetwork{
		transport: t,
		nextID:    1,
		nodes:     make(map[int64]string),
		sockets:   make(map[int64]transport.Socket),
		peers:     NewPeerMap(),
	}
}

type PeerMap struct {
	sync.RWMutex
	peers map[int64]string
}

func NewPeerMap() *PeerMap {
	return &PeerMap{peers: make(map[int64]string)}
}

func (pm *PeerMap) GetPeers(excludeID int64) map[int64]string {
	pm.RLock()
	defer pm.RUnlock()

	result := make(map[int64]string)
	for id, addr := range pm.peers {
		if id != excludeID {
			result[id] = addr
		}
	}
	return result
}

func (pm *PeerMap) Add(id int64, addr string) {
	pm.Lock()
	defer pm.Unlock()
	pm.peers[id] = addr
}

func (n *TransportNetwork) JoinNetwork() (NetworkInterface, error) {
	bindAddr := "127.0.0.1:0"
	n.mu.Lock()
	defer n.mu.Unlock()

	socket, err := n.transport.CreateSocket(bindAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket: %w", err)
	}

	id := n.nextID
	n.nextID++
	addr := socket.GetAddress()

	n.sockets[id] = socket
	n.peers.Add(id, addr)

	return NewSocketNetwork(socket, id, n.peers), nil
}

type SocketNetwork struct {
	socket transport.Socket
	id     int64
	// peers     map[int64]string // mapping from ID to address
	peers     *PeerMap
	incoming  chan []byte
	stop      chan struct{}
	recvWg    sync.WaitGroup
	recvMutex sync.Mutex
}

// NewSocketNetwork creates a new SocketNetwork with a given socket and ID.
// `peers` maps IDs to addresses (host:port).
func NewSocketNetwork(socket transport.Socket, id int64, peers *PeerMap) *SocketNetwork {

	net := &SocketNetwork{
		socket:   socket,
		id:       id,
		peers:    peers,
		incoming: make(chan []byte, 100),
		stop:     make(chan struct{}),
	}

	net.recvWg.Add(1)
	go net.receiver()

	return net
}

func (n *SocketNetwork) Send(msg []byte, to int64) error {
	addrMap := n.peers.GetPeers(n.id)
	addr, ok := addrMap[to]
	if !ok {
		return fmt.Errorf("unknown peer ID: %d", to)
	}

	header := transport.NewHeader(n.socket.GetAddress(), addr, addr)
	packet := transport.Packet{
		Header: &header,
		Msg:    &transport.Message{Type: "bytes", Payload: msg},
	}

	timeout := time.Second
	err := n.socket.Send(addr, packet, timeout)
	if err != nil {
		return err
	}

	return nil
}

func (n *SocketNetwork) Broadcast(msg []byte) error {
	addrMap := n.peers.GetPeers(-1)
	timeout := time.Second
	for _, addr := range addrMap {
		// I AM sending to myself as well

		header := transport.NewHeader(n.socket.GetAddress(), addr, addr)
		packet := transport.Packet{
			Header: &header,
			Msg:    &transport.Message{Type: "bytes", Payload: msg},
		}

		err := n.socket.Send(addr, packet, timeout)
		if err != nil {
			return err
		}
	}

	return nil
}

func (n *SocketNetwork) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-n.stop:
		return nil, fmt.Errorf("network stopped")
	case msg := <-n.incoming:
		n.recvMutex.Lock()
		n.recvMutex.Unlock()
		return msg, nil
	}
}

func (n *SocketNetwork) GetID() int64 {
	return n.id
}

func (n *SocketNetwork) GetSent() [][]byte {
	n.recvMutex.Lock()
	defer n.recvMutex.Unlock()
	pkts := n.socket.GetOuts()
	sent := make([][]byte, len(pkts))
	for i, pkt := range n.socket.GetOuts() {
		sent[i] = pkt.Msg.Payload
	}
	return sent
}

func (n *SocketNetwork) GetReceived() [][]byte {
	n.recvMutex.Lock()
	pkts := n.socket.GetIns()
	received := make([][]byte, len(pkts))
	for i, pkt := range pkts {
		received[i] = pkt.Msg.Payload
	}
	return received
}

func (n *SocketNetwork) Close() error {
	close(n.stop)
	n.recvWg.Wait()
	return nil
}

// Internal goroutine to receive packets.
func (n *SocketNetwork) receiver() {
	defer n.recvWg.Done()
	timeout := time.Second

	for {
		select {
		case <-n.stop:
			return
		default:
			pkt, err := n.socket.Recv(timeout)
			if err != nil {
				if _, ok := err.(transport.TimeoutError); ok {
					continue
				}
				// Other errors: assume fatal
				return
			}

			if pkt.Msg == nil {
				continue
			}

			// Payload is protobuf endcoded message
			n.incoming <- pkt.Msg.Payload
		}
	}
}
