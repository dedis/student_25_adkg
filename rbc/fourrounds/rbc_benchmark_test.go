package fourrounds

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strconv"
	"student_25_adkg/networking"
	"student_25_adkg/reedsolomon"
	"student_25_adkg/transport/udp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runWithParameters(t *testing.T, ctx context.Context, threshold int, message []byte, messageLength int) (time.Duration, int) {
	// Config
	network := networking.NewTransportNetwork(udp.NewUDP())

	nbNodes := 3*threshold + 1
	r := 2

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		nIface, err := network.JoinNetwork()
		require.NoError(t, err)
		rs := reedsolomon.NewBWCodes(messageLength, nbNodes)
		node := NewTestNode(nIface, NewFourRoundRBC(defaultPredicate, sha256.New(), threshold, nIface, rs, r, nIface.GetID()))
		nodes[i] = node
	}

	// Run RBC and check the result
	start := time.Now()
	runBroadcastWithContext(t, ctx, nodes, nbNodes, message)
	end := time.Now()
	elapsed := end.Sub(start)

	received := len(nodes[0].GetReceived())
	return elapsed, received
}

func runBroadcastWithContext(t *testing.T, ctx context.Context, nodes []*TestNode, nbNodes int, msg []byte) {
	// Create a wait group to wait for all bracha instances to finish
	wg := sync.WaitGroup{}
	n1 := nodes[0]
	for i := 1; i < nbNodes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := nodes[i].rbc.Listen(ctx)
			if err != nil {
				// Log
				t.Logf("Error listening: %v", err)
			}
			t.Logf("Node %d done", i)
		}()
	}
	// Start RBC
	err := n1.rbc.RBroadcast(ctx, msg)
	t.Log("Broadcast complete")
	require.NoError(t, err)

	wg.Wait()
}

func TestRBC_Benchmark_Message(t *testing.T) {
	minThreshold := 2
	maxThreshold := 23

	steps := maxThreshold - minThreshold + 1

	message := generateMessage(2)
	messageLength := 2

	thresholds := make([]int, steps)
	durations := make([]time.Duration, steps)
	messagesCounts := make([]int, steps)

	idx := 0
	for threshold := minThreshold; threshold <= maxThreshold; threshold++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		duration, messagesCount := runWithParameters(t, ctx, threshold, message, messageLength)

		cancel()

		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			break
		}

		thresholds[idx] = threshold
		durations[idx] = duration
		messagesCounts[idx] = messagesCount
		idx++
	}

	saveToCSV(durations, thresholds, messagesCounts)
}

func saveToCSV(timings []time.Duration, thresholds, messagesCounts []int) {
	// Open file for writing
	file, err := os.Create("output.csv")
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer file.Close()

	// Create CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	writer.Write([]string{"Timing(ms)", "Threshold", "MessageCount"})

	// Write data rows
	for i := 0; i < len(timings); i++ {
		row := []string{
			strconv.FormatInt(timings[i].Milliseconds(), 10),
			strconv.Itoa(thresholds[i]),
			strconv.Itoa(messagesCounts[i]),
		}
		if err := writer.Write(row); err != nil {
			fmt.Println("Error writing row:", err)
		}
	}

	fmt.Println("CSV file written successfully.")
}
