package adkg

type Phase int8

const (
	sharing Phase = iota
	agreement
	randomnessExtraction
	keyDerivation
)

type ADKG struct {
	Phase
}

func NewADKG() *ADKG {
	return &ADKG{Phase: sharing}
}
