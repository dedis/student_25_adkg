package fourRounds

type Message interface {
	Type() MessageType
}

type ProposeMessage[M any] struct {
	Content M
}

func (p *ProposeMessage[M]) Type() MessageType {
	return PROPOSE
}

type EchoMessage struct {
	Mi []byte
	H  []byte
}

func (e *EchoMessage) Type() MessageType {
	return ECHO
}

type ReadyMessage EchoMessage

func (r *ReadyMessage) Type() MessageType {
	return READY
}

type MessageType uint32

const (
	PROPOSE MessageType = iota
	ECHO
	READY
)
