package agreement

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"student_25_adkg/agreement/typedefs"
	"student_25_adkg/networking"

	"google.golang.org/protobuf/proto"
)

type ABANode struct {
	BVManager     *InstanceManager[BVBroadcast, BVBroadcastConfig]
	SBVManager    *InstanceManager[SBVBroadcast, SBVBroadcastConfig]
	ABAManager    *InstanceManager[ABA, ABAConfig]
	CCoinManageer *InstanceManager[CommonCoin, CommonCoinConfig]
}

func (b *ABANode) HandleBVMessage(msg *typedefs.ABAEnvelope_BvMsg) (int, bool, error) {
	return b.BVManager.GetOrCreate(msg.BvMsg.GetRoundId()).HandleMessage(msg.BvMsg)
}

func (b *ABANode) HandleAuxMessage(msg *typedefs.ABAEnvelope_AuxMsg) error {
	return b.SBVManager.GetOrCreate(msg.AuxMsg.GetRoundId()).HandleMessage(msg.AuxMsg)
}

func (b *ABANode) HandleAuxSetMessage(msg *typedefs.ABAEnvelope_AuxSetMsg) error {
	abaRoundId, err := ABARoundUIDFromString(msg.AuxSetMsg.GetRoundId())
	if err != nil {
		logger.Error().Msgf("failed to convert %s to ABARoundUID, %v", msg.AuxSetMsg.GetRoundId(), err)
	}
	return b.ABAManager.GetOrCreate(strconv.Itoa(abaRoundId.AgreementID)).HandleMessage(msg.AuxSetMsg)
}

func (b *ABANode) HandleCoinMessage(msg *typedefs.ABAEnvelope_CoinMsg) error {
	return b.CCoinManageer.GetOrCreate(msg.CoinMsg.BroadcastId).HandleMessage(msg.CoinMsg)
}

func (b *ABANode) GetBV(id string) *BVBroadcast {
	return b.BVManager.GetOrCreate(id)
}

func NewABANode(conf ABACommonConfig) *ABANode {

	bvBroadcastConfig := &BVBroadcastConfig{
		NParticipants: conf.NParticipants,
		Threshold:     conf.Threshold,
		NodeID:        conf.NodeID,
		BroadcastFn:   conf.BroadcastFn,
	}

	bvManager := NewInstanceManager(bvBroadcastConfig, NewBVBroadcastFromConfig,
		func(base *BVBroadcastConfig, id string) *BVBroadcastConfig {
			base.RoundID = id
			return base
		},
	)

	sbvBroadcastConfig := &SBVBroadcastConfig{
		NParticipants: conf.NParticipants,
		Threshold:     conf.Threshold,
		NodeID:        conf.NodeID,
		BroadcastFn:   conf.BroadcastFn,
		BVManager:     bvManager,
	}

	sbvManager := NewInstanceManager(sbvBroadcastConfig, NewSBVBroadcastFromConfig,
		func(base *SBVBroadcastConfig, id string) *SBVBroadcastConfig {
			base.RoundID = id
			return base
		},
	)

	coinConfig := &CommonCoinConfig{
		NParticipants: conf.NParticipants,
		Threshold:     conf.NParticipants - conf.Threshold,
		NodeID:        conf.NodeID,
		BroadcastFn:   conf.BroadcastFn,
		LocalShare:    conf.LocalShare,
		PubCommitment: conf.PubCommitment,
		Scheme:        conf.Scheme,
	}

	coinManager := NewInstanceManager(coinConfig, NewCommonCoinFromConfig,
		func(base *CommonCoinConfig, id string) *CommonCoinConfig {
			base.BroadcastID = id // same as string of ABARoundUID
			return base
		},
	)

	abaConfig := &ABAConfig{
		NParticipants: conf.NParticipants,
		Threshold:     conf.Threshold,
		NodeID:        conf.NodeID,
		BroadcastFn:   conf.BroadcastFn,
		SBVManager:    sbvManager,
		CCoinManageer: coinManager,
	}

	abaManager := NewInstanceManager(abaConfig, NewABAFromConf,
		func(base *ABAConfig, id string) *ABAConfig {
			var err error
			// abaUID, err := ABARoundUIDFromString(id)
			// if err != nil {
			// 	logger.Error().Msgf("failed to convert %s to ABARoundUID: %v", id, err)
			// }
			// base.AgreementID = int(abaUID.AgreementID)
			base.AgreementID, err = strconv.Atoi(id)
			if err != nil {
				logger.Error().Msgf("failed to convert %s to int", id)
			}
			return base
		})

	return &ABANode{
		BVManager:     bvManager,
		SBVManager:    sbvManager,
		ABAManager:    abaManager,
		CCoinManageer: coinManager,
	}
}

type ABAStream struct {
	iface networking.NetworkInterface[[]byte]
}

func NewABAStream(iface networking.NetworkInterface[[]byte]) *ABAStream {
	return &ABAStream{
		iface: iface,
	}
}

func EncodeABAMessage(msg proto.Message) ([]byte, error) {
	switch m := msg.(type) {
	case *typedefs.BVMessage:
		return proto.Marshal(&typedefs.ABAEnvelope{
			Msg: &typedefs.ABAEnvelope_BvMsg{BvMsg: m},
		})
	case *typedefs.AuxMessage:
		return proto.Marshal(&typedefs.ABAEnvelope{
			Msg: &typedefs.ABAEnvelope_AuxMsg{AuxMsg: m},
		})
	case *typedefs.AuxSetMessage:
		return proto.Marshal(&typedefs.ABAEnvelope{
			Msg: &typedefs.ABAEnvelope_AuxSetMsg{AuxSetMsg: m},
		})
	case *typedefs.CoinMessage:
		return proto.Marshal(&typedefs.ABAEnvelope{
			Msg: &typedefs.ABAEnvelope_CoinMsg{CoinMsg: m},
		})
	default:
		return nil, fmt.Errorf("unsupported message type: %T", msg)
	}
}

func (abas *ABAStream) Broadcast(msg proto.Message) error {
	bs, err := EncodeABAMessage(msg)
	if err != nil {
		return err
	}
	err = abas.iface.Broadcast(bs)
	return err
}

// to make sbv broadcast send different values in aux to different participants
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
				err = abas.iface.Send(bs, int64(pid))
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
			err = abas.iface.Send(bs, int64(pid))
			return err
		}
		return nil
	}
}

func (abas *ABAStream) Listen(ctx context.Context, node *ABANode) {
	go func() {
		for {
			bs, err := abas.iface.Receive(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				panic(err)
			}

			var env typedefs.ABAEnvelope
			if err := proto.Unmarshal(bs, &env); err != nil {
				// return nil, fmt.Errorf("failed to unmarshal ABAEnvelope: %w", err)
			}

			switch m := env.Msg.(type) {
			case *typedefs.ABAEnvelope_BvMsg:
				_, _, err := node.HandleBVMessage(m)
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
				// return nil, fmt.Errorf("unknown ABAEnvelope message type")
			}
		}
	}()
}
