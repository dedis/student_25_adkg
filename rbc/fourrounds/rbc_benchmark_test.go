package fourrounds

import (
	"bytes"
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

func createNetwork(network networking.Network, threshold int) ([]*TestNode, error) {
	nbNodes := 3*threshold + 1

	// Set up the nodes
	nodes := make([]*TestNode, nbNodes)
	for i := 0; i < nbNodes; i++ {
		nIface, err := network.JoinNetwork()
		if err != nil {
			return nil, err
		}
		rs := reedsolomon.NewBWCodes(threshold+1, nbNodes)
		node := NewTestNode(nIface, NewFourRoundRBC(sha256.New(), threshold, nIface, rs, nIface.GetID()))
		nodes[i] = node
	}
	return nodes, nil
}

func startNodes(ctx context.Context, t require.TestingT, nodes []*TestNode, expectedErr error) {
	for _, node := range nodes {
		go func() {
			err := node.rbc.Listen(ctx)
			require.ErrorIs(t, err, expectedErr)
		}()
	}
}

func waitForResult(ctx context.Context, t require.TestingT, nodes []*TestNode,
	messageHash []byte, expectSuccess bool) *sync.WaitGroup {
	wg := &sync.WaitGroup{}
	// Allow retrying to get the instance
	for _, node := range nodes {
		wg.Add(1)
		go func() {
			for {
				// Wait for the state
				select {
				case <-ctx.Done():
					require.False(t, expectSuccess)
					wg.Done()
					return
				case state, ok := <-node.rbc.GetFinishedChannel():
					require.True(t, ok)
					if bytes.Equal(state.Identifier(), messageHash) {
						require.Equal(t, expectSuccess, state.Success())
						wg.Done()
						return
					}
				}
			}
		}()
	}

	return wg
}

func Benchmark_Threshold20(b *testing.B) {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	threshold := 20

	message, hash := generateMessage(threshold + 1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		network := networking.NewTransportNetwork(udp.NewUDP())
		nodes, err := createNetwork(network, threshold)
		require.NoError(b, err)
		// Should finish within 5 seconds
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		// Start the nodes
		startNodes(ctx, b, nodes[1:], context.Canceled)

		dealer := nodes[0]

		wg := waitForResult(ctx, b, nodes, hash, true)
		// Start RBC
		_, err = dealer.rbc.RBroadcast(message)
		require.NoError(b, err)

		wg.Wait()
		cancel()
	}

	b.ReportAllocs()
}

func TestRBC_TimingsMessages(b *testing.T) {
	b.Skip("Skipping this test by default. Run explicitly if needed.")
	minThreshold := 5
	maxThreshold := 35

	stepSize := 5

	messageLength := 2
	message, messageHash := generateMessage(messageLength)

	size := (maxThreshold-minThreshold)/stepSize + 1
	thresholds := make([]int, size)
	durations := make([]time.Duration, size)
	messagesSent := make([][][]byte, size)

	idx := 0
	for threshold := minThreshold; threshold <= maxThreshold; threshold += stepSize {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		network := networking.NewTransportNetwork(udp.NewUDP())
		nodes, err := createNetwork(network, threshold)
		require.NoError(b, err)

		start := time.Now()
		runBroadcast(ctx, b, nodes, message, messageHash, context.Canceled, true)
		end := time.Now()

		cancel()

		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			break
		}

		thresholds[idx] = threshold
		durations[idx] = end.Sub(start)
		messagesSent[idx] = make([][]byte, 0)
		for _, node := range nodes {
			messagesSent[idx] = append(messagesSent[idx], node.GetReceived()...)
		}
		idx++
	}

	saveToCSV(b, durations, thresholds, messagesSent)
}

func saveToCSV(t require.TestingT, timings []time.Duration, thresholds []int, messages [][][]byte) {
	// Open file for writing
	file, err := os.Create("output.csv")
	require.NoError(t, err)
	defer file.Close()

	// Create CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	err = writer.Write([]string{"Timing(ms)", "Threshold", "MessageCount", "MessagesBytesCount"})
	if err != nil {
		panic(err)
	}

	// Write data rows
	for i := 0; i < len(timings); i++ {

		messagesCount := len(messages[i])
		bytesCount := int64(0)
		for _, m := range messages[i] {
			bytesCount += int64(len(m))
		}

		row := []string{
			strconv.FormatInt(timings[i].Milliseconds(), 10),
			strconv.Itoa(thresholds[i]),
			strconv.Itoa(messagesCount),
			strconv.FormatInt(bytesCount, 10),
		}
		if err := writer.Write(row); err != nil {
			continue
		}
	}
}
