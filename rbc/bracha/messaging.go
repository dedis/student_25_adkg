package bracha

type Message struct {
	MsgType MessageType
	Content bool
}

func NewBrachaMessage(msgType MessageType, content bool) *Message {
	return &Message{
		MsgType: msgType,
		Content: content,
	}
}

type MessageType uint32

const (
	PROPOSE MessageType = iota
	ECHO
	READY
)
