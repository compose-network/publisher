package contracts

import (
	"context"
	_ "embed"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
)

// DisputeGameFactory ABI JSON embedded at compile time
//
//go:embed abi/dispute_game_factory.json
var disputeGameFactoryABIJSON string

var (
	_ Binding = (*DisputeGameFactoryBinding)(nil)
)

const composeGameType uint32 = 5555

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

	contractAddr = "0xf3f81abf097d7cc92f8dc5e4f136691485111de1"
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

// GameType returns the compose dispute game type identifier used when creating games.
func (b *DisputeGameFactoryBinding) GameType() uint32 {
	return composeGameType
}

// BuildPublishWithProofCalldata encodes a superblock and proof for DisputeGameFactory.create()
// according to the settlement layer specification.
func (b *DisputeGameFactoryBinding) BuildPublishWithProofCalldata(ctx context.Context, sb *store.Superblock, proof []byte, outputs *proofs.SuperblockAggOutputs) ([]byte, error) {
	if sb == nil {
		return nil, fmt.Errorf("superblock cannot be nil")
	}
	if len(proof) == 0 {
		return nil, fmt.Errorf("proof cannot be empty")
	}

	// Encode the extraData as (bytes outputs, bytes proof)
	extraData, err := encodeExtraData(b.toSuperblockAggregationOutputs(outputs), proof)
	if err != nil {
		return nil, fmt.Errorf("failed to encode extradata: %v", err)
	}

	// rootClaim - parent superblock batch hash.
	rootClaim := sb.ParentHash
	data, err := b.abi.Pack("create", composeGameType, rootClaim, extraData)
	if err != nil {
		return nil, fmt.Errorf("failed to pack DisputeGameFactory.create calldata: %w", err)
	}

	return data, nil
}

func encodeExtraData(superBlockAggOutputs superblockAggregationOutputs, proof []byte) ([]byte, error) {
	superblockType, _ := abi.NewType("tuple", "SuperblockAggregationOutputs", []abi.ArgumentMarshaling{
		{Name: "superblockNumber", Type: "uint256"},
		{Name: "parentSuperblockBatchHash", Type: "bytes32"},
		{Name: "bootInfo", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
			{Name: "l1Head", Type: "bytes32"},
			{Name: "l2PreRoot", Type: "bytes32"},
			{Name: "l2PostRoot", Type: "bytes32"},
			{Name: "l2BlockNumber", Type: "uint64"},
			{Name: "rollupConfigHash", Type: "bytes32"},
		}},
	})

	bytesType, _ := abi.NewType("bytes", "", nil)

	arguments := abi.Arguments{
		{Type: superblockType},
		{Type: bytesType},
	}

	packed, err := arguments.Pack(superBlockAggOutputs, proof)
	if err != nil {
		return nil, err
	}

	return packed, nil
}

// toSuperblockAggregationOutputs converts prover outputs to SuperblockAggregationOutputs
func (b *DisputeGameFactoryBinding) toSuperblockAggregationOutputs(outputs *proofs.SuperblockAggOutputs) superblockAggregationOutputs {
	var bootInfo []bootInfoStruct
	superblockNumber := new(big.Int)
	var parentSuperblockBatchHash common.Hash

	if outputs != nil {
		bootInfo = make([]bootInfoStruct, 0, len(outputs.BootInfo))
		for _, proverBootInfo := range outputs.BootInfo {
			bootInfo = append(bootInfo, bootInfoStruct{
				L1Head:           common.HexToHash(proverBootInfo.L1Head),
				L2PreRoot:        common.HexToHash(proverBootInfo.L2PreRoot),
				L2PostRoot:       common.HexToHash(proverBootInfo.L2PostRoot),
				L2BlockNumber:    proverBootInfo.L2BlockNumber,
				RollupConfigHash: common.HexToHash(proverBootInfo.RollupConfigHash),
			})
		}

		if outputs.SuperblockNumber != "" {
			superblockNumber.SetString(outputs.SuperblockNumber, 0)
		}

		parentSuperblockBatchHash = common.HexToHash(outputs.ParentSuperblockBatchHash)
	}

	return superblockAggregationOutputs{
		SuperblockNumber:          superblockNumber,
		ParentSuperblockBatchHash: parentSuperblockBatchHash,
		BootInfo:                  bootInfo,
	}
}
