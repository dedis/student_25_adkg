package testing

import "student_25_adkg/networking"

func SetupNetwork(network networking.Network, nbNodes int) ([]networking.NetworkInterface, error) {
	nodes := make([]networking.NetworkInterface, nbNodes)
	for i := 0; i < nbNodes; i++ {
		node, err := network.JoinNetwork()
		if err != nil {
			return nil, err
		}
		nodes[i] = node
	}

	return nodes, nil
}
