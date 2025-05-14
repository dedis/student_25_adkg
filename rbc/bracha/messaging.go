package bracha

import "student_25_adkg/rbc"

type Message struct {
	InstanceID rbc.InstanceIdentifier
	MsgType    MessageType
	Content    bool
}

func NewBrachaMessage(identifier rbc.InstanceIdentifier, msgType MessageType, content bool) *Message {
	return &Message{
		MsgType:    msgType,
		Content:    content,
		InstanceID: identifier,
	}
}

type MessageType uint32

const (
	PROPOSE MessageType = iota
	ECHO
	READY
)
