package agreement

type ABA struct {
	est       int
	round     int
	views     map[int]map[int]BinSet // round -> stage[0,1,2] -> view
	binValues map[int]BinSet         // round -> binvalues
}

func (a *ABA) Round() error {
	a.round++
	// a.views[a.round][0] =

	return nil
}

// func (b *ABA) HandleMessage(msg AuxSetessage) (error) {
// }
