package adkg

type Phase int8
type SendMode int8

const (
	sharing Phase = iota
	agreement
	randomnessExtraction
	keyDerivation
)

const (
	BROADCAST SendMode = iota
	UNICAST
)

type Message struct {
	Phase   Phase
	content []byte
}

type Packet struct {
	SendMode    SendMode
	Destination int
	Message     *Message
}

type ADKG interface {
	RunADKG() error
	HandleMessage(Message) ([]Packet, error)
}
