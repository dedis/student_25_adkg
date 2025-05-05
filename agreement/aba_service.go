package agreement

import (
	"fmt"
	"strconv"
	"student_25_adkg/agreement/typedefs"

	"google.golang.org/protobuf/proto"
)

type ABAService struct {
	BVManager     *InstanceManager[BVBroadcast, BVBroadcastConfig]
	SBVManager    *InstanceManager[SBVBroadcast, SBVBroadcastConfig]
	ABAManager    *InstanceManager[ABA, ABAConfig]
	CCoinManageer *InstanceManager[CommonCoin, CommonCoinConfig]
}

func (abaS *ABAService) HandleBVMessage(msg *typedefs.ABAEnvelope_BvMsg) error {
	return abaS.BVManager.GetOrCreate(msg.BvMsg.GetRoundId()).HandleMessage(msg.BvMsg)
}

func (abaS *ABAService) HandleAuxMessage(msg *typedefs.ABAEnvelope_AuxMsg) error {
	return abaS.SBVManager.GetOrCreate(msg.AuxMsg.GetRoundId()).HandleMessage(msg.AuxMsg)
}

func (abaS *ABAService) HandleAuxSetMessage(msg *typedefs.ABAEnvelope_AuxSetMsg) error {
	abaRoundID, err := ABARoundUIDFromString(msg.AuxSetMsg.GetRoundId())
	if err != nil {
		logger.Error().Msgf("failed to convert %s to ABARoundUID, %v", msg.AuxSetMsg.GetRoundId(), err)
	}
	return abaS.ABAManager.GetOrCreate(strconv.Itoa(abaRoundID.AgreementID)).HandleMessage(msg.AuxSetMsg)
}

func (abaS *ABAService) HandleCoinMessage(msg *typedefs.ABAEnvelope_CoinMsg) error {
	return abaS.CCoinManageer.GetOrCreate(msg.CoinMsg.BroadcastId).HandleMessage(msg.CoinMsg)
}

func (abaS *ABAService) GetBV(id string) *BVBroadcast {
	return abaS.BVManager.GetOrCreate(id)
}

func NewABAService(conf ABACommonConfig) *ABAService {

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
			base.AgreementID, err = strconv.Atoi(id)
			if err != nil {
				logger.Error().Msgf("failed to convert %s to int", id)
			}
			return base
		})

	return &ABAService{
		BVManager:     bvManager,
		SBVManager:    sbvManager,
		ABAManager:    abaManager,
		CCoinManageer: coinManager,
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
