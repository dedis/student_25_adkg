package rbc

type MessageType int8

const (
	PROPOSE MessageType = iota
	ECHO
	READY
)

type RBC[M any] interface {
	// Deal creates the PROPOSE message to be broadcast
	Deal(M) (MessageType, M)
	// HandleMessage handles a message received. It returns a message to send if send is true, and an output value to
	// for the algorithm if output is true and the value is stored in out.
	HandleMessage(MessageType, M) (msgType MessageType, val M, send bool, out M, output bool)
}
