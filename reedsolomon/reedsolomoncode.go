package reedsolomon

// Encoding represents an encoded part of a message containing its byte value and its index in the encoded message
type Encoding struct {
	Idx int
	Val []byte
}

type RSEncoder interface {
	// Encode encodes the given message into a list of Encodings
	Encode(msg []byte) ([]Encoding, error)
}

type RSDecoder interface {
	// Decode takes a set of Encodings to be decoded. Returns the decoded bytes or an error
	Decode(msg []Encoding) ([]byte, error)
}

// RSCodes is an interface representing an implementation of Reed-Solomon codes that
// can encode and decode a message
type RSCodes interface {
	RSEncoder
	RSDecoder
}
