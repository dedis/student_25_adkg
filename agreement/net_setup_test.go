package agreement

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"student_25_adkg/agreement/typedefs"
	"student_25_adkg/networking"

	"google.golang.org/protobuf/proto"
)

type ABAStream struct {
	Iface networking.NetworkInterface
}

func NewABAStream(iface networking.NetworkInterface) *ABAStream {
	return &ABAStream{
		Iface: iface,
	}
}

func (abas *ABAStream) Broadcast(msg proto.Message) error {
	bs, err := EncodeABAMessage(msg)
	if err != nil {
		return err
	}
	err = abas.Iface.Broadcast(bs)
	return err
}

// A hacky testing function to simulate byzantine behaviour of sbv broadcast.
// Make sbv broadcast send different values in aux to different participants.
func (abas *ABAStream) RandomSBVBroadcast(destNodes []int) func(msg proto.Message) error {
	return func(msg proto.Message) error {
		for _, pid := range destNodes {
			auxMsg, ok := msg.(*typedefs.AuxMessage)
			if ok {
				auxMsg.BinValue = int32(rand.Int() % 2)

				bs, err := EncodeABAMessage(auxMsg)
				if err != nil {
					return err
				}
				err = abas.Iface.Send(bs, int64(pid))
				return err
			}
			bvMsg, ok := msg.(*typedefs.BVMessage)
			if !ok {
				return fmt.Errorf("failed to convert proto.Message to BVMessage: %T", msg)
			}
			bs, err := EncodeABAMessage(bvMsg)
			if err != nil {
				return err
			}
			err = abas.Iface.Send(bs, int64(pid))
			if err != nil {
				return err
			}
		}
		return nil
	}
}

func (abas *ABAStream) Listen(ctx context.Context, node *ABAService) {
	go func() {
		for {
			bs, err := abas.Iface.Receive(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				panic(err)
			}

			var env typedefs.ABAEnvelope
			if err := proto.Unmarshal(bs, &env); err != nil {
				panic(fmt.Errorf("failed to unmarshal ABAEnvelope: %w", err))
			}

			switch m := env.Msg.(type) {
			case *typedefs.ABAEnvelope_BvMsg:
				err := node.HandleBVMessage(m)
				if err != nil {
					panic(err)
				}
			case *typedefs.ABAEnvelope_AuxMsg:
				err := node.HandleAuxMessage(m)
				if err != nil {
					panic(err)
				}
			case *typedefs.ABAEnvelope_AuxSetMsg:
				err := node.HandleAuxSetMessage(m)
				if err != nil {
					panic(err)
				}
			case *typedefs.ABAEnvelope_CoinMsg:
				err := node.HandleCoinMessage(m)
				if err != nil {
					panic(err)
				}
			default:
				panic(fmt.Errorf("unknown ABAEnvelope message type"))
			}
		}
	}()
}

func DecodeMessagesByType(encodedMessages [][]byte) (
	[]*typedefs.BVMessage,
	[]*typedefs.AuxMessage,
	[]*typedefs.AuxSetMessage,
	[]*typedefs.CoinMessage,
	error,
) {
	var bvMessages []*typedefs.BVMessage
	var auxMessages []*typedefs.AuxMessage
	var auxSetMessages []*typedefs.AuxSetMessage
	var coinMessages []*typedefs.CoinMessage

	for _, bs := range encodedMessages {
		var env typedefs.ABAEnvelope
		if err := proto.Unmarshal(bs, &env); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to unmarshal ABAEnvelope: %w", err)
		}

		switch m := env.Msg.(type) {
		case *typedefs.ABAEnvelope_BvMsg:
			bvMessages = append(bvMessages, m.BvMsg)
		case *typedefs.ABAEnvelope_AuxMsg:
			auxMessages = append(auxMessages, m.AuxMsg)
		case *typedefs.ABAEnvelope_AuxSetMsg:
			auxSetMessages = append(auxSetMessages, m.AuxSetMsg)
		case *typedefs.ABAEnvelope_CoinMsg:
			coinMessages = append(coinMessages, m.CoinMsg)
		default:
			return nil, nil, nil, nil, fmt.Errorf("unknown ABAEnvelope message type")
		}
	}

	return bvMessages, auxMessages, auxSetMessages, coinMessages, nil
}
