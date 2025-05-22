package fourrounds

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"os"
	"strconv"
	"student_25_adkg/networking"
	"student_25_adkg/reedsolomon"
	"student_25_adkg/transport/udp"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func createNetwork(threshold int) (networking.Network, []*TestNode, error) {
	network := networking.NewTransportNetwork(udp.NewUDP())

	nbNodes := 3*threshold + 1

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		nIface, err := network.JoinNetwork()
		if err != nil {
			return network, nil, err
		}
		rs := reedsolomon.NewBWCodes(threshold+1, nbNodes)
		node := NewTestNode(nIface, NewFourRoundRBC(defaultPredicate, sha256.New(), threshold, nIface, rs, nIface.GetID()))
		nodes[i] = node
	}
	return network, nodes, nil
}

func startNodes(ctx context.Context, log func(string, ...any), nodes []*TestNode) *sync.WaitGroup {
	// Create a wait group to wait for all bracha instances to finish
	wg := sync.WaitGroup{}
	for _, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := node.rbc.Listen(ctx)
			if err != nil {
				// Log
				log("Error listening: %v", err)
			}
		}()
	}
	return &wg
}

func runWithParameters(ctx context.Context, t *testing.B, threshold int,
	message []byte) (time.Duration, int) {
	// Config
	_, nodes, err := createNetwork(threshold)
	require.NoError(t, err)

	// Start all nodes but the dealer (idx 0)
	wg := startNodes(ctx, t.Logf, nodes[1:])

	dealer := nodes[0]

	// Run RBC and check the result
	start := time.Now()
	// Start RBC
	err = dealer.rbc.RBroadcast(ctx, message)
	require.NoError(t, err)
	t.Log("Broadcast complete")

	wg.Wait()
	end := time.Now()
	elapsed := end.Sub(start)

	received := len(nodes[0].GetReceived())
	return elapsed, received
}

func Benchmark_Threshold20(b *testing.B) {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	threshold := 20

	message := generateMessage(threshold + 1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, nodes, err := createNetwork(threshold)
		require.NoError(b, err)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)

		// Start the nodes
		wg := startNodes(ctx, b.Logf, nodes[1:])

		dealer := nodes[0]

		// Start RBC
		err = dealer.rbc.RBroadcast(ctx, message)
		require.NoError(b, err)

		wg.Wait()
		cancel()
	}

	b.ReportAllocs()
}

func BenchmarkRBC_TimingsMessages(t *testing.B) {
	t.Skip("Skipping this test by default. Run explicitly if needed.")
	minThreshold := 2
	maxThreshold := 30

	messageLength := 2
	message := generateMessage(messageLength)

	size := maxThreshold - minThreshold + 1
	thresholds := make([]int, size)
	durations := make([]time.Duration, size)
	messagesCounts := make([]int, size)

	idx := 0
	for threshold := minThreshold; threshold <= maxThreshold; threshold++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		duration, messagesCount := runWithParameters(ctx, t, threshold, message)

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
		return
	}
	defer file.Close()

	// Create CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	err = writer.Write([]string{"Timing(ms)", "Threshold", "MessageCount"})
	if err != nil {
		panic(err)
	}

	// Write data rows
	for i := 0; i < len(timings); i++ {
		row := []string{
			strconv.FormatInt(timings[i].Milliseconds(), 10),
			strconv.Itoa(thresholds[i]),
			strconv.Itoa(messagesCounts[i]),
		}
		if err := writer.Write(row); err != nil {
			continue
		}
	}
}
