package udp

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"student_25_adkg/transport"

	"student_25_adkg/internal/traffic"

	"github.com/rs/zerolog/log"
)

const bufSize = 65000

// NewUDP returns a new udp transport implementation.
var globalTrafficLock sync.RWMutex

func NewUDP() transport.Transport {
	globalTrafficLock.Lock()
	defer globalTrafficLock.Unlock()
	return &UDP{
		traffic: traffic.NewTraffic(),
	}
}

// UDP implements a transport layer using UDP
//
// - implements transport.Transport
type UDP struct {
	sync.RWMutex
	traffic *traffic.Traffic
}

// CreateSocket implements transport.Transport
func (n *UDP) CreateSocket(address string) (transport.ClosableSocket, error) {
	var conn *net.UDPConn

	// btw why should I lock here?
	// just copied it from transport/channel.go
	n.Lock()
	conn, err := n.tryCreateSocketPort(address)
	n.Unlock()

	if err != nil {
		log.Error().Msgf("could not create a socket with address:port %s", address)
		return &Socket{}, fmt.Errorf("could not create a socket %w", err)
	}

	address = conn.LocalAddr().String()
	log.Info().Msgf("success: created a socket with address:port %s", address)

	return &Socket{
		UDP:    n,
		myAddr: address,
		conn:   conn,
		ins:    packets{},
		outs:   packets{},
	}, nil
}

func (n *UDP) tryCreateSocketPort(address string) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		log.Info().Msgf("failed to resolve UDPAddr")
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)

	if err != nil {
		log.Error().Msgf("failed to start listening net.ListenUDP on port %d", addr.Port)
		return nil, err
	}

	return conn, nil
}

// Socket implements a network socket using UDP.
//
// - implements transport.Socket
// - implements transport.ClosableSocket
type Socket struct {
	*UDP
	myAddr string
	conn   *net.UDPConn
	ins    packets
	outs   packets
}

// Close implements transport.Socket. It returns an error if already closed.
func (s *Socket) Close() error {
	err := s.conn.Close()
	if err != nil {
		log.Error().Msgf("error: trying to close a closed connection")
		return fmt.Errorf("error: trying to close a closed connection %w", err)
	}
	log.Info().Msgf("Socket closed without an error.")
	return nil
}

// Send implements transport.Socket
func (s *Socket) Send(dest string, pkt transport.Packet, timeout time.Duration) error {
	addr, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		log.Error().Msgf("error trying to resolve udp addr %s", dest)
		return err
	}

	bytePkt, err := pkt.Copy().Marshal()
	if err != nil {
		log.Error().Msgf("error marshalling a packet")
		return err
	}

	var deadline time.Time
	if timeout != 0 {
		deadline = time.Now().Add(timeout)
	} else {
		deadline = time.Time{}
	}

	err = s.conn.SetWriteDeadline(deadline)
	if err != nil {
		log.Error().Msgf("error setting write deadline")
		return err
	}

	n, err := s.conn.WriteToUDP(bytePkt, addr)
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// logs too verbose
			// log.Error().Msgf("timeout sending to %s", addr)
			return transport.TimeoutError(0)
		}
		return err
	}
	log.Debug().Msgf("%d bytes sent to %s", n, dest)

	s.outs.add(pkt)
	log.Debug().Msgf("%s sent a message to %s", s.myAddr, dest)
	s.traffic.LogSent(pkt.Header.RelayedBy, dest, pkt)

	return nil
}

// Recv implements transport.Socket. It blocks until a packet is received, or
// the timeout is reached. In the case the timeout is reached, return a
// TimeoutErr.
func (s *Socket) Recv(timeout time.Duration) (transport.Packet, error) {
	rBuff := make([]byte, bufSize)
	var deadline time.Time
	if timeout != 0 {
		deadline = time.Now().Add(timeout)
	} else {
		deadline = time.Time{}
	}
	err := s.conn.SetReadDeadline(deadline)

	if err != nil {
		log.Error().Msgf("error setting read deadline")
		return transport.Packet{}, err

	}
	n, _, err := s.conn.ReadFromUDP(rBuff)
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// logs would bee too verbose
			// log.Error().Msgf("timeout reading %s", s.myAddr)
			return transport.Packet{}, transport.TimeoutError(0)
		}

		log.Error().Msgf("%d bytes received, something went wront", n)
		return transport.Packet{}, err
	}
	log.Debug().Msgf("%d bytes read", n)

	// unmarshal and add to ins
	var pkt transport.Packet

	err = pkt.Unmarshal(rBuff[:n])
	if err != nil {
		log.Error().Msgf("Recv: Error unmarshalling a packet")
		return transport.Packet{}, err
	}
	s.ins.add(pkt)
	s.traffic.LogRecv(pkt.Header.RelayedBy, s.myAddr, pkt)

	return pkt, nil
}

// GetAddress implements transport.Socket. It returns the address assigned. Can
// be useful in the case one provided a :0 address, which makes the system use a
// random free port.
func (s *Socket) GetAddress() string {
	return s.myAddr
}

// GetIns implements transport.Socket
func (s *Socket) GetIns() []transport.Packet {
	return s.ins.getAll()
}

// GetOuts implements transport.Socket
func (s *Socket) GetOuts() []transport.Packet {
	return s.outs.getAll()
}

// I've just copied a struct from transport/channels.go
// Can it be taken out to some utils package?
// future refactoring

type packets struct {
	sync.Mutex
	data []transport.Packet
}

func (p *packets) add(pkt transport.Packet) {
	p.Lock()
	p.addUnsafe(pkt)
	p.Unlock()
}

func (p *packets) addUnsafe(pkt transport.Packet) {
	p.data = append(p.data, pkt.Copy())
}

func (p *packets) getAll() []transport.Packet {
	p.Lock()
	defer p.Unlock()

	res := make([]transport.Packet, len(p.data))

	// for i, pkt := range p.data {
	// 	// why copy?
	// 	// res[i] = pkt.Copy()
	// 	res[i] = pkt
	// }
	copy(res, p.data)

	return res
}
