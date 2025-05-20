package networking

type Network interface {
	JoinNetwork() (NetworkInterface, error)
}
