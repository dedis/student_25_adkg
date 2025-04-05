package agreement

type MessageType int

const (
	BValMsg MessageType = iota
	AuxMsg
	CoinMsg
)

// I know it should take abstraction
type IMessage interface {
	Marshal() Message
	// Unmarshal(*Message)
}

type Message struct {
	Type    string
	Payload []byte
	// protobuff stuff here?
}

// TODO finish
func (m *Message) unpackAuxMsg() (*AUXMessage, error) {
	// payload => into aux msg and so on
	aux := AUXMessage{}
	return &aux, nil
}

// double protobuf? gRPC?
// something like marshal and unmarshal for msgs, then protobuf transport msg into packet?
// transpRumorMsg, err := n.conf.MessageRegistry.MarshalMessage(&rumorsMessage)

type BVMessage struct {
	sourceNode int
	binValue   int
}

func (m *BVMessage) Marshal() Message {
	return Message{} // TODO
}

func (m *AUXMessage) Marshal() Message {
	return Message{} // TODO
}

func (aux *AUXMessage) Unmarshal(m *Message) {
	// aux.binValue =
	// aux.sourceNode =
}

type AUXMessage struct {
	sourceNode int
	binValue   int
}
