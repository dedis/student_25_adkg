package tcp

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"student_25_adkg/transport"

	"github.com/rs/zerolog/log"
)

const bufSize = 65000

func NewTCP() transport.Transport {
	return &TCP{}
}

type TCP struct{}

func (t *TCP) CreateSocket(address string) (transport.ClosableSocket, error) {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("could not listen on %s: %w", address, err)
	}

	socket := &Socket{
		listener: ln,
		myAddr:   ln.Addr().String(),
		conns:    make(map[string]net.Conn),
		incoming: make(chan transport.Packet, 1000),
		closing:  make(chan struct{}),
	}

	go socket.acceptLoop()

	return socket, nil
}

type Socket struct {
	listener net.Listener
	myAddr   string
	conns    map[string]net.Conn
	mutex    sync.Mutex
	incoming chan transport.Packet
	closing  chan struct{}
	closed   bool
}

func (s *Socket) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closing:
				return
			default:
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *Socket) handleConnection(conn net.Conn) {
	defer conn.Close()

	for {
		buf := make([]byte, bufSize) // Use a local buffer to avoid sharing
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Debug().Msgf("%v", err)
			}
			return
		}

		var pkt transport.Packet
		err = pkt.Unmarshal(buf[:n])
		if err != nil {
			log.Info().Msgf("error unmarshaling pkt %s %v", pkt, err)
			continue
		}

		select {
		case s.incoming <- pkt:
		default:
			log.Info().Msgf("drop packet from %s because incoming buffer is full", pkt)
		}
	}
}

func (s *Socket) Send(dest string, pkt transport.Packet, timeout time.Duration) error {
	s.mutex.Lock()
	conn, exists := s.conns[dest]
	s.mutex.Unlock()

	if !exists {
		var err error
		conn, err = net.DialTimeout("tcp", dest, timeout)
		if err != nil {
			return err
		}

		s.mutex.Lock()
		s.conns[dest] = conn
		s.mutex.Unlock()
	}

	err := conn.SetWriteDeadline(time.Now().Add(timeout))
	if err != nil {
		log.Error().Msgf("failed to set write deadline")
		return err
	}

	bytes, err := pkt.Marshal()
	if err != nil {
		log.Error().Msgf("failed to marshal msg")
		return err
	}
	_, err = conn.Write(bytes)
	if err != nil {
		if errors.Is(err, net.ErrWriteToConnected) {
			return transport.TimeoutError(timeout)
		}
		return err
	}

	return nil
}

func (s *Socket) Recv(timeout time.Duration) (transport.Packet, error) {
	if timeout == 0 {
		pkt := <-s.incoming
		return pkt, nil
	}

	select {
	case pkt := <-s.incoming:
		return pkt, nil
	case <-time.After(timeout):
		return transport.Packet{}, transport.TimeoutError(timeout)
	}
}

func (s *Socket) GetAddress() string {
	return s.myAddr
}

func (s *Socket) Close() error {
	if s.closed {
		return fmt.Errorf("already closed")
	}
	s.closed = true
	close(s.closing)
	s.listener.Close()

	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, conn := range s.conns {
		conn.Close()
	}
	return nil
}

func (s *Socket) GetIns() []transport.Packet {
	return nil // not tracked
}

func (s *Socket) GetOuts() []transport.Packet {
	return nil // not tracked
}
