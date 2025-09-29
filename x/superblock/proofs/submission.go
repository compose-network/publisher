package proofs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Submission represents a proof payload provided by a single op-succinct instance.
type Submission struct {
	SuperblockNumber uint64             `json:"superblock_number"`
	SuperblockHash   common.Hash        `json:"superblock_hash"`
	ChainID          uint32             `json:"chain_id"`
	L1Head           common.Hash        `json:"l1_head"`
	Aggregation      AggregationOutputs `json:"aggregation_outputs"`
	L2StartBlock     uint64             `json:"l2_start_block"`
	AggVerifyingKey  json.RawMessage    `json:"agg_vk"`
	MailboxInfo      MailboxInfoStruct  `json:"mailbox_info"`
	Proof            []byte             `json:"proof,omitempty"`
	ReceivedAt       time.Time          `json:"received_at"`
}

func (s *Submission) MailboxInfoToString() string {
	mailboxJSON, err := json.Marshal(s.MailboxInfo)
	if err != nil {
		fmt.Println("Error converting s.MailboxInfo toString") // TODO: return err
		return ""
	}

	return string(mailboxJSON)
}
