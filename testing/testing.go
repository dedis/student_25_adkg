package testing

import "student_25_adkg/networking"

func SetupNetwork(nbNodes int) (networking.Network, []networking.NetworkInterface, error) {
	network := networking.NewFakeNetwork()

	nodes := make([]networking.NetworkInterface, nbNodes)
	for i := 0; i < nbNodes; i++ {
		node, err := network.JoinNetwork()
		if err != nil {
			return nil, nil, err
		}
		nodes[i] = node
	}

	return network, nodes, nil
}
