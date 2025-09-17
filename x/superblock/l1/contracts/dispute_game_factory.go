package contracts

import (
	"context"
	_ "embed"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
)

// DisputeGameFactory ABI JSON embedded at compile time
//
//go:embed abi/dispute_game_factory.json
var disputeGameFactoryABIJSON string

var (
	_ Binding = (*DisputeGameFactoryBinding)(nil)
)

// DisputeGameFactoryBinding provides functionality to interact with DisputeGameFactory
// smart contracts for creating dispute games with superblock proofs.
type DisputeGameFactoryBinding struct {
	address common.Address
	abi     abi.ABI
}

// NewDisputeGameFactoryBinding creates a new DisputeGameFactoryBinding instance with
// the specified contract address. It parses the embedded ABI and validates
// the contract address.
func NewDisputeGameFactoryBinding(contractAddr string) (*DisputeGameFactoryBinding, error) {
	if strings.TrimSpace(contractAddr) == "" {
		return nil, fmt.Errorf("contract address cannot be empty")
	}

	parsedABI, err := abi.JSON(strings.NewReader(disputeGameFactoryABIJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse DisputeGameFactory ABI: %w", err)
	}

	return &DisputeGameFactoryBinding{
		address: common.HexToAddress(contractAddr),
		abi:     parsedABI,
	}, nil
}

// Address returns the Ethereum address of the DisputeGameFactory contract.
func (b *DisputeGameFactoryBinding) Address() common.Address {
	return b.address
}

// ABI returns the parsed ABI of the DisputeGameFactory contract.
func (b *DisputeGameFactoryBinding) ABI() abi.ABI {
	return b.abi
}

// BuildPublishWithProofCalldata encodes a superblock and proof for DisputeGameFactory.create()
// according to the settlement layer specification.
func (b *DisputeGameFactoryBinding) BuildPublishWithProofCalldata(
	_ context.Context,
	sb *store.Superblock,
	proof []byte,
) ([]byte, error) {
	if sb == nil {
		return nil, fmt.Errorf("superblock cannot be nil")
	}
	if len(proof) == 0 {
		return nil, fmt.Errorf("proof cannot be empty")
	}

	// Create SuperblockAggregationOutputs structure
	superblockAggOutputs := b.toSuperblockAggregationOutputs(sb)

	// Encode the extraData as (SuperblockAggregationOutputs, bytes proof)
	extraData, err := abi.Arguments{
		{Type: mustParseType("tuple", buildSuperblockAggregationOutputsType())},
		{Type: mustParseType("bytes", nil)},
	}.Pack(superblockAggOutputs, proof)
	if err != nil {
		return nil, fmt.Errorf("failed to encode extraData: %w", err)
	}

	// COMPOSE_GAME_TYPE from ComposeDisputeGame.sol
	gameType := uint32(5555)

	// rootClaim is the parent superblock batch hash
	rootClaim := sb.ParentHash

	// Pack the create() function call
	data, err := b.abi.Pack("create", gameType, rootClaim, extraData)
	if err != nil {
		return nil, fmt.Errorf("failed to pack DisputeGameFactory.create calldata: %w", err)
	}

	return data, nil
}

// toSuperblockAggregationOutputs converts a store.Superblock to SuperblockAggregationOutputs
// format expected by the settlement layer.
func (b *DisputeGameFactoryBinding) toSuperblockAggregationOutputs(sb *store.Superblock) superblockAggregationOutputs {
	// Convert L2 blocks to BootInfoStruct array
	bootInfo := make([]bootInfoStruct, 0, len(sb.L2Blocks))
	for _, block := range sb.L2Blocks {
		if block == nil {
			continue
		}

		// Extract required fields for BootInfoStruct
		bootInfo = append(bootInfo, bootInfoStruct{
			L1Head:           sb.ParentHash,                             // L1 head from superblock context
			L2PreRoot:        common.BytesToHash(block.ParentBlockHash), // Previous state root
			L2PostRoot:       common.BytesToHash(block.BlockHash),       // Post-execution state root
			L2BlockNumber:    block.BlockNumber,                         // L2 block number
			RollupConfigHash: common.BytesToHash(block.ChainId),         // Chain ID as config hash
		})
	}

	return superblockAggregationOutputs{
		SuperblockNumber:          big.NewInt(int64(sb.Number)),
		ParentSuperblockBatchHash: sb.ParentHash,
		BootInfo:                  bootInfo,
	}
}
