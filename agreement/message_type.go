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

type AUXMessage struct {
	sourceNode int
	binValue   int
}

func (aux *AUXMessage) Unmarshal(m *Message) {
	// aux.binValue =
	// aux.sourceNode =
}

func (aux *AUXSetMessage) Marshal(m *Message) {

}

type AUXSetMessage struct {
	sourceNode int
	round      int
	binSet     [2]bool
}
