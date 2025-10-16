package l1

// Config holds Ethereum L1 integration configuration
type Config struct {
	// Enable L1 publishing
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// RPC endpoint to an Ethereum node. Prefer WS for subscriptions.
	RPCEndpoint string `mapstructure:"rpc_endpoint" yaml:"rpc_endpoint"`

	// Addresses (hex) for on-chain contracts used by the SP.
	RegistryContract string `mapstructure:"registry_contract" yaml:"registry_contract"`

	// DisputeGameFactory for proof-enabled superblock submission
	DisputeGameFactory string `mapstructure:"dispute_game_factory" yaml:"dispute_game_factory"`

	// Chain configuration
	ChainID       uint64 `mapstructure:"chain_id"       yaml:"chain_id"`
	Confirmations uint64 `mapstructure:"confirmations"  yaml:"confirmations"`  // for Confirmed status
	FinalityDepth uint64 `mapstructure:"finality_depth" yaml:"finality_depth"` // for Finalized status

	// Gas/fees configuration (EIP-1559)
	UseEIP1559        bool   `mapstructure:"use_eip1559"          yaml:"use_eip1559"`
	MaxFeePerGasWei   string `mapstructure:"max_fee_per_gas_wei"  yaml:"max_fee_per_gas_wei"`  // optional cap
	MaxPriorityFeeWei string `mapstructure:"max_priority_fee_wei" yaml:"max_priority_fee_wei"` // optional tip cap
	GasLimitBufferPct uint64 `mapstructure:"gas_limit_buffer_pct" yaml:"gas_limit_buffer_pct"` // add buffer to estimates

	// Signing configuration
	// One of SharedPublisherPkHex OR an external Signer must be provided at runtime.
	SharedPublisherPkHex string `mapstructure:"shared_publisher_pk_hex" yaml:"shared_publisher_pk_hex" env:"L1_SHARED_PUBLISHER_PK_HEX"` //nolint:lll // ok
	FromAddress          string `mapstructure:"from_address"            yaml:"from_address"            env:"L1_FROM_ADDRESS"`            //nolint:lll // ok, optional
}

func DefaultConfig() Config {
	return Config{
		RPCEndpoint:       "ws://localhost:8546", // Default to websocket for event subscriptions
		Confirmations:     2,
		FinalityDepth:     64,
		UseEIP1559:        true,
		GasLimitBufferPct: 15,
	}
}
