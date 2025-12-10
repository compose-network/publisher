package sequencer

import (
	"time"

	"github.com/compose-network/specs/compose"
)

// Config holds sequencer coordinator configuration
type Config struct {
	ChainID compose.ChainID `json:"chain_id"`
	// Sequencer-specific settings
	BlockTimeout         time.Duration `json:"block_timeout"`
	MaxLocalTxs          int           `json:"max_local_txs"`
	SCPTimeout           time.Duration `json:"scp_timeout"`
	EnableCIRCValidation bool          `json:"enable_circ_validation"`
}

// DefaultConfig returns sensible defaults for sequencer
func DefaultConfig(chainID compose.ChainID) Config {
	return Config{
		ChainID:              chainID,
		BlockTimeout:         30 * time.Second,
		MaxLocalTxs:          1000,
		SCPTimeout:           10 * time.Second,
		EnableCIRCValidation: true,
	}
}
