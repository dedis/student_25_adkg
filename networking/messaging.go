package networking

type Message[T any, C any] struct {
	MsgType    T
	MsgContent C
}

func NewMessage[T any, C any](msgType T, msgContent C) *Message[T, C] {
	return &Message[T, C]{
		MsgType:    msgType,
		MsgContent: msgContent,
	}
}
