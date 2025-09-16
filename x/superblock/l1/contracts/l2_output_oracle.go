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

// L2 output oracle ABI JSON embedded at compile time
//
//go:embed abi/l2_output_oracle.json
var l2OutputOracleABIJSON string

var (
	_ Binding      = (*L2OutputOracleBinding)(nil)
	_ ProofBinding = (*L2OutputOracleBinding)(nil)
)

// l2BlockArg represents the ABI structure for an L2 block argument.
type l2BlockArg struct {
	Slot            uint64   `abi:"slot"`
	ChainId         []byte   `abi:"chainId"`
	BlockNumber     uint64   `abi:"blockNumber"`
	BlockHash       []byte   `abi:"blockHash"`
	ParentBlockHash []byte   `abi:"parentBlockHash"`
	IncludedXts     [][]byte `abi:"included_xts"`
	Block           []byte   `abi:"block"`
}

// superBlockArg represents the ABI structure for a superblock argument.
type superBlockArg struct {
	BlockNumber       uint64       `abi:"blockNumber"`
	Slot              uint64       `abi:"slot"`
	ParentBlockHash   []byte       `abi:"parentBlockHash"`
	MerkleRoot        []byte       `abi:"merkleRoot"`
	Timestamp         *big.Int     `abi:"timestamp"`
	L2Blocks          []l2BlockArg `abi:"l2Blocks"`
	IncludedXTs       [][]byte     `abi:"_includedXTs"`
	TransactionHashes []byte       `abi:"_transactionHashes"`
	Status            uint8        `abi:"status"`
}

// L2OutputOracleBinding provides functionality to interact with L2 output oracle
// smart contracts. It encapsulates the contract address and ABI for encoding
// proposeL2Output(SuperBlock) and proposeL2OutputWithProof(SuperBlock, proof) calls.
type L2OutputOracleBinding struct {
	address common.Address
	abi     abi.ABI
}

// NewL2OutputOracleBinding creates a new L2OutputOracleBinding instance with
// the specified contract address. It parses the embedded ABI and validates
// the contract address.
//
// Returns an error if the contract address is empty or if the ABI cannot be parsed.
func NewL2OutputOracleBinding(contractAddr string) (*L2OutputOracleBinding, error) {
	if strings.TrimSpace(contractAddr) == "" {
		return nil, fmt.Errorf("contract address cannot be empty")
	}

	parsedABI, err := abi.JSON(strings.NewReader(l2OutputOracleABIJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	return &L2OutputOracleBinding{
		address: common.HexToAddress(contractAddr),
		abi:     parsedABI,
	}, nil
}

// Address returns the Ethereum address of the L2 output oracle contract.
func (b *L2OutputOracleBinding) Address() common.Address {
	return b.address
}

// ABI returns the parsed ABI of the L2 output oracle contract.
func (b *L2OutputOracleBinding) ABI() abi.ABI {
	return b.abi
}

// BuildPublishCalldata encodes a superblock into calldata for the proposeL2Output
// smart contract method. It converts the superblock into the appropriate ABI format
// and packs it using the contract's ABI.
//
// Returns an error if the superblock is nil or if ABI packing fails.
func (b *L2OutputOracleBinding) BuildPublishCalldata(_ context.Context, sb *store.Superblock) ([]byte, error) {
	if sb == nil {
		return nil, fmt.Errorf("superblock cannot be nil")
	}

	arg := b.toSuperBlockArg(sb)
	data, err := b.abi.Pack("proposeL2Output", arg)
	if err != nil {
		return nil, fmt.Errorf("failed to pack proposeL2Output calldata: %w", err)
	}

	return data, nil
}

// BuildPublishWithProofCalldata encodes a superblock and proof for proposeL2OutputWithProof(superBlock, proof).
// TODO: merge later
func (b *L2OutputOracleBinding) BuildPublishWithProofCalldata(
	_ context.Context,
	sb *store.Superblock,
	proof []byte,
) ([]byte, error) {
	if sb == nil {
		return nil, fmt.Errorf("superblock cannot be nil")
	}
	arg := b.toSuperBlockArg(sb)
	data, err := b.abi.Pack("proposeL2OutputWithProof", arg, proof)
	if err != nil {
		return nil, fmt.Errorf("failed to pack proposeL2OutputWithProof calldata: %w", err)
	}
	return data, nil
}

// toSuperBlockArg converts a store.Superblock into the ABI-compatible superBlockArg
// structure. It handles the conversion of nested L2 blocks and ensures proper
// format for smart contract interaction.
func (b *L2OutputOracleBinding) toSuperBlockArg(sb *store.Superblock) superBlockArg {
	// Convert L2 blocks to ABI format
	l2Blocks := make([]l2BlockArg, 0, len(sb.L2Blocks))
	for _, block := range sb.L2Blocks {
		if block == nil {
			continue
		}
		l2Blocks = append(l2Blocks, l2BlockArg{
			Slot:            block.Slot,
			ChainId:         block.ChainId,
			BlockNumber:     block.BlockNumber,
			BlockHash:       block.BlockHash,
			ParentBlockHash: block.ParentBlockHash,
			IncludedXts:     block.IncludedXts,
			Block:           block.Block,
		})
	}

	incl := make([][]byte, 0, len(sb.IncludedXTs))
	for _, h := range sb.IncludedXTs {
		incl = append(incl, h.Bytes())
	}

	return superBlockArg{
		BlockNumber:       sb.Number,
		Slot:              sb.Slot,
		ParentBlockHash:   sb.ParentHash.Bytes(),
		MerkleRoot:        sb.MerkleRoot.Bytes(),
		Timestamp:         big.NewInt(sb.Timestamp.Unix()),
		L2Blocks:          l2Blocks,
		IncludedXTs:       incl,
		TransactionHashes: packTransactionHashes(sb.IncludedXTs),
		Status:            mapSuperblockStatus(sb.Status),
	}
}

// packTransactionHashes converts a slice of common.Hash to a single byte slice
// by concatenating their byte representations.
func packTransactionHashes(hashes []common.Hash) []byte {
	if len(hashes) == 0 {
		return nil
	}

	result := make([]byte, 0, 32*len(hashes))
	for _, hash := range hashes {
		result = append(result, hash.Bytes()...)
	}

	return result
}

// mapSuperblockStatus converts a store.SuperblockStatus to its corresponding
// uint8 representation used in smart contracts.
func mapSuperblockStatus(status store.SuperblockStatus) uint8 {
	switch status {
	case store.SuperblockStatusSubmitted:
		return 1
	case store.SuperblockStatusConfirmed:
		return 2
	case store.SuperblockStatusFinalized:
		return 3
	case store.SuperblockStatusRolledBack:
		return 4
	case store.SuperblockStatusPending:
		fallthrough
	default:
		return 0
	}
}
