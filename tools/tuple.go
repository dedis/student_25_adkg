package tools

type Tuple[X any, Y any] struct {
	el1 X
	el2 Y
}

func NewTuple[X any, Y any](x X, y Y) *Tuple[X, Y] {
	return &Tuple[X, Y]{
		el1: x,
		el2: y,
	}
}

func (t *Tuple[X, Y]) GetEl1() X {
	return t.el1
}
func (t *Tuple[X, Y]) GetEl2() Y {
	return t.el2
}
