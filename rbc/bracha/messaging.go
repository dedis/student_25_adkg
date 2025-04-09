package bracha

type BrachaMessage struct {
	MsgType MessageType
	Content bool
}

func NewBrachaMessage(msgType MessageType, content bool) *BrachaMessage {
	return &BrachaMessage{
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
