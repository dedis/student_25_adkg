package messaging

type Message[M any] interface {
	Type() int8
	Content() M
}
