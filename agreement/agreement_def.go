package agreement

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
)

const (
	Zero = iota
	One
	UndecidedBinVal
)

type ABACommonConfig struct {
	NParticipants int
	Threshold     int
	NodeID        int
	BroadcastFn   func(proto.Message) error
}

func (conf *ABACommonConfig) CopySetNodeID(nodeID int) ABACommonConfig {
	newConf := *conf
	newConf.NodeID = nodeID
	return newConf
}

type ABARoundUID struct {
	AgreementID int
	Round       int
	Stage       int
	Prefix      string
}

func (id ABARoundUID) String() string {
	return fmt.Sprintf("ABAp%son%dr%ds%d", id.Prefix, id.AgreementID, id.Round, id.Stage)
}

func ABARoundUIDFromString(s string) (ABARoundUID, error) {
	// Expected format: "ABAp<prefix>on<AgreementID>r<Round>s<Stage>"
	if !strings.HasPrefix(s, "ABAp") {
		return ABARoundUID{}, fmt.Errorf("invalid format: missing 'ABAp' prefix")
	}
	s = strings.TrimPrefix(s, "ABAp")
	prefixAndRest := strings.SplitN(s, "on", 2)
	if len(prefixAndRest) != 2 {
		return ABARoundUID{}, fmt.Errorf("invalid format: expected 'on' separator")
	}
	prefix := prefixAndRest[0]
	parts := strings.Split(prefixAndRest[1], "r")
	if len(parts) != 2 {
		return ABARoundUID{}, fmt.Errorf("invalid format: expected 'r' separator")
	}
	agreementID, err := strconv.Atoi(parts[0])
	if err != nil {
		return ABARoundUID{}, fmt.Errorf("invalid AgreementID: %v", err)
	}
	roundAndStage := strings.Split(parts[1], "s")
	if len(roundAndStage) != 2 {
		return ABARoundUID{}, fmt.Errorf("invalid format: expected 's' separator")
	}
	round, err := strconv.Atoi(roundAndStage[0])
	if err != nil {
		return ABARoundUID{}, fmt.Errorf("invalid Round: %v", err)
	}
	stage, err := strconv.Atoi(roundAndStage[1])
	if err != nil {
		return ABARoundUID{}, fmt.Errorf("invalid Stage: %v", err)
	}
	return ABARoundUID{
		AgreementID: agreementID,
		Round:       round,
		Stage:       stage,
		Prefix:      prefix,
	}, nil
}
